#!/usr/bin/python3

'''Wrapper generator for libbpf'''

import json
from argparse import ArgumentParser

PREAMBLE = '''#include <bpf/bpf.h>
#include <dlfcn.h>
#include <pthread.h>

'''

INIT = '''
static bool init_done = false;
static void* default_rtld = NULL;
static pthread_mutex_t init_lock = PTHREAD_MUTEX_INITIALIZER;

static void inline init_dl(void)
{
    pthread_mutex_lock(&init_lock);
    if (!init_done) {
        default_rtld = dlopen("libbpf.so", RTLD_LOCAL);
        init_done = true;
    }
    pthread_mutex_unlock(&init_lock);
}
'''

DECLARE_TEMPLATE = '''
static  {func_return} (*real_{func_name})({func_args}) = NULL;
'''

INVOKE_NON_VOID_TEMPLATE = '''
{func_return} {func_name}({func_args})
{{
    init_dl();
    if (!real_{func_name}) {{
        real_{func_name} = dlsym(default_rtld, "{func_name}");
    }}
    return real_{func_name}({processed_args});
}}

'''

INVOKE_VOID_TEMPLATE = '''
{func_return} {func_name}({func_args})
{{
    init_dl();
    if (!real_{func_name}) {{
        real_{func_name} = dlsym(default_rtld, "{func_name}");
    }}
    real_{func_name}({processed_args});
}}

'''

class ExportedFunction():
    '''Visible function from BPF API'''
    def __init__(self, func_def):

        self.func_def = func_def
        left = self.func_def.find("(")
        right = self.func_def.find(")")

        tokens = self.func_def[:left].split()
        # last token is the function name
        self.func_name = tokens[-1]

        # ignore first which is LIBBPF_API, return is in the middle
        self.func_return = " ".join(tokens[1:-1])

        self.func_args = self.func_def[left+1:right]
        res = []
        for token in self.func_args.split(","):
            res.append(token.split()[-1].lstrip("*"))
        self.processed_args = ", ".join(res)

    def dump_wrapper(self):
        '''Dump default wrapper'''

        if self.func_return == "void":
            return INVOKE_VOID_TEMPLATE.format(
                func_return=self.func_return,
                func_name=self.func_name,
                func_args=self.func_args,
                processed_args=self.processed_args
            )
        return INVOKE_NON_VOID_TEMPLATE.format(
            func_return=self.func_return,
            func_name=self.func_name,
            func_args=self.func_args,
            processed_args=self.processed_args
        )

    def dump_declaration(self):
        '''Dump function declaration'''

        return DECLARE_TEMPLATE.format(
            func_return=self.func_return,
            func_name=self.func_name,
            func_args=self.func_args
        )

def main():
    aparser = ArgumentParser(description=main.__doc__)
    aparser.add_argument(
        '--config',
        help='config file in json format',
        type=str)
    
    args = vars(aparser.parse_args())
    config = json.load(open(args.get("config")))

    bpfh = open(config["include"], "r")
    start = False
    func_def = ""
    defs = []
    for line in bpfh.read().splitlines():
        if line.find("LIBBPF_API") != -1:
            start = True
        if start:
            func_def = func_def + line + "\n"
        if line.find(";") != -1 and start:
            for skip in config["exceptions"]:
                if func_def.find(skip):
                    start = False
                    break
            if start:
                defs.append(ExportedFunction(func_def))
            start = False
            func_def = ""
    bpfh.close()

    print(PREAMBLE)
    print(INIT)
    
    for func_def in defs:
        print(func_def.dump_declaration())

    for func_def in defs:
        print(func_def.dump_wrapper())

if __name__ == "__main__":
    main()

