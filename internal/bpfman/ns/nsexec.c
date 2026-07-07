// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman
//
// nsexec.c - Mount namespace switching before Go runtime starts.
//
// This code runs as a GCC constructor, executing before main() and before
// Go's runtime initializes. At this point the process is single-threaded,
// allowing us to call setns(CLONE_NEWNS) which requires a single-threaded
// process.
//
// The approach is inspired by runc's libcontainer/nsenter but simplified
// for bpfman's needs (only mount namespace switching for uprobes).
//
// Environment variables:
//   _BPFMAN_MNT_NS       - Path to mount namespace file (e.g.,
//   /proc/1234/ns/mnt)
//                         If not set, nsexec() returns immediately and Go
//                         starts normally.
//   _BPFMAN_NS_LOG_LEVEL - Log level: "debug", "info", "error", or unset
//   (default: error only)

#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <sched.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <time.h>
#include <unistd.h>

// Log levels
#define LOG_LEVEL_NONE 0
#define LOG_LEVEL_ERROR 1
#define LOG_LEVEL_INFO 2
#define LOG_LEVEL_DEBUG 3

static int log_level = LOG_LEVEL_ERROR;

static void init_log_level(void) {
  const char *level = getenv("_BPFMAN_NS_LOG_LEVEL");
  if (level == NULL) {
    log_level = LOG_LEVEL_ERROR;
  } else if (strcmp(level, "debug") == 0) {
    log_level = LOG_LEVEL_DEBUG;
  } else if (strcmp(level, "info") == 0) {
    log_level = LOG_LEVEL_INFO;
  } else if (strcmp(level, "error") == 0) {
    log_level = LOG_LEVEL_ERROR;
  } else if (strcmp(level, "none") == 0) {
    log_level = LOG_LEVEL_NONE;
  }
}

static void log_msg(int level, const char *level_str, const char *fmt, ...) {
  if (level > log_level)
    return;

  struct timespec ts;
  clock_gettime(CLOCK_REALTIME, &ts);

  // Format: nsexec[pid]: LEVEL: message
  fprintf(stderr, "nsexec[%d]: %s: ", getpid(), level_str);

  va_list ap;
  va_start(ap, fmt);
  vfprintf(stderr, fmt, ap);
  va_end(ap);

  fprintf(stderr, "\n");
  fflush(stderr);
}

#define log_debug(fmt, ...)                                                    \
  log_msg(LOG_LEVEL_DEBUG, "DEBUG", fmt, ##__VA_ARGS__)
#define log_info(fmt, ...) log_msg(LOG_LEVEL_INFO, "INFO", fmt, ##__VA_ARGS__)
#define log_error(fmt, ...)                                                    \
  log_msg(LOG_LEVEL_ERROR, "ERROR", fmt, ##__VA_ARGS__)

static ino_t get_ns_inode(const char *ns_type) {
  char path[64];
  struct stat st;
  snprintf(path, sizeof(path), "/proc/self/ns/%s", ns_type);
  if (stat(path, &st) < 0)
    return 0;
  return st.st_ino;
}

// nsexec is called by the constructor before Go runtime starts.
// If _BPFMAN_MNT_NS is set, it switches to that mount namespace.
// Otherwise, it returns immediately and Go continues normally.
void nsexec(void) {
  const char *ns_path;
  int fd;
  ino_t orig_mnt_ns, new_mnt_ns;

  init_log_level();

  ns_path = getenv("_BPFMAN_MNT_NS");
  if (ns_path == NULL || ns_path[0] == '\0') {
    log_debug("_BPFMAN_MNT_NS not set, returning to Go runtime");
    return;
  }

  log_info("namespace switch requested");
  log_debug("target namespace path: %s", ns_path);
  log_debug("current pid: %d, ppid: %d", getpid(), getppid());

  orig_mnt_ns = get_ns_inode("mnt");
  log_debug("current mount namespace inode: %lu", (unsigned long)orig_mnt_ns);

  log_debug("opening target namespace file: %s", ns_path);
  fd = open(ns_path, O_RDONLY | O_CLOEXEC);
  if (fd < 0) {
    log_error("failed to open mount namespace %s: %s (errno=%d)", ns_path,
              strerror(errno), errno);
    _exit(1);
  }
  log_debug("opened namespace fd: %d", fd);

  log_info("calling setns(fd=%d, CLONE_NEWNS) for %s", fd, ns_path);
  if (setns(fd, CLONE_NEWNS) < 0) {
    log_error("setns(%s, CLONE_NEWNS) failed: %s (errno=%d)", ns_path,
              strerror(errno), errno);
    close(fd);
    _exit(1);
  }

  close(fd);

  new_mnt_ns = get_ns_inode("mnt");
  log_info("setns succeeded: mount namespace changed %lu -> %lu",
           (unsigned long)orig_mnt_ns, (unsigned long)new_mnt_ns);

  unsetenv("_BPFMAN_MNT_NS");
  log_debug("cleared _BPFMAN_MNT_NS environment variable");

  log_info("returning to Go runtime in new mount namespace");
}
