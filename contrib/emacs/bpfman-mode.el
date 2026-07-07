;;; bpfman-mode.el --- Major mode for bpfman-shell scripts -*- lexical-binding: t; -*-

;; Version: 0.9.0
;; Keywords: languages, bpf
;; Package-Requires: ((emacs "25.1"))

;; This file provides a major mode for editing bpfman-shell scripts
;; (.bpfman files).

;;; Commentary:

;; bpfman-mode provides structural syntax highlighting, canonical
;; formatting via `bpfman-shell --fmt', and xref definitions via
;; `bpfman-shell --symbols'. See `emacs/syntax-gallery.bpfman' for
;; editor-facing syntax examples and the bpfman-shell user guide for
;; language documentation.
;;
;; Usage:
;;
;;   (require 'bpfman-mode)

;;; Code:

(require 'cl-lib)
(require 'json)
(require 'seq)
(require 'subr-x)
(require 'xref)

(defgroup bpfman nil
  "Major mode for bpfman-shell scripts."
  :group 'languages
  :prefix "bpfman-")

;; Implementation note:
;;
;; This mode is organised as fetch / compute / execute, mirroring the
;; rest of the project. Fetch reads Emacs state -- buffer text, point,
;; buffer-file-name, buffer-modified-p. Compute turns that into plain
;; data -- tokens, semantic roles, interpolation segments, process
;; queries, and xref matches -- that ERT can assert directly. Execute
;; interprets that data at the boundary: text properties, process
;; calls, and xref locations.
;;
;; The scanner is the buffer-backed exception inside recognition: it
;; reads buffer text and advances point as it goes. Point-sensitive
;; callers such as the xref backend therefore wrap it in
;; save-excursion; the higher-level results are still plain data.
;;
;; "Adapter" elsewhere in this file refers to bpfman-shell syntax such
;; as the file:$value adapter-ref token, not this implementation
;; pattern.

;; ---- Word sets ----

(defconst bpfman--def-param-types '("number" "string" "bool")
  "Accepted type annotations on def parameters.
Mirrors DefParamTypes in cmd/bpfman-shell/shell/syntax/ast_stmt.go.")

(defconst bpfman--commands
  (let ((ht (make-hash-table :test 'equal)))
    (dolist (w '(;; binding and lifecycle
                 "let" "guard" "defer"
                 ;; control flow
                 "if" "elif" "else"
                 "foreach" "break" "continue"
                 ;; poll/retry and value-publishing exit from def
                 "poll" "retry" "return"
                 ;; user-defined commands
                 "def"
                 ;; domain gateway
                 "bpfman"
                 ;; assertion keywords
                 "assert" "require"
                 ;; pure-builtin call producers (jq, range, zip,
                 ;; u32le, u64le); the parser-visible registry
                 ;; that mirrors shell/syntax/purebuiltin.go
                 "jq" "range" "zip" "u32le" "u64le"
                 ;; shell builtins registered by
                 ;; internal/builtins/*.go
                 "defs" "exec" "file" "fire" "import" "jobs"
                 "kill" "net" "print" "reap" "start" "tempdir"
                 "trace" "wait"))
      (puthash w t ht))
    ht)
  "Hash table of top-level bpfman-shell keywords and commands.")

(defconst bpfman--subcommands
  (let ((ht (make-hash-table :test 'equal)))
    (dolist (w '(;; domain commands (after "bpfman" prefix)
                 "dispatcher" "doctor" "gc" "link" "list" "load"
                 "program" "programs" "show"
                 ;; subcommands
                 "attach" "checkup" "delete" "detach" "explain" "file"
                 "get" "image"
                 "status" "temp" "unload"
                 ;; attach types
                 "fentry" "fexit" "kprobe" "tc" "tcx" "tracepoint"
                 "uprobe" "xdp"
                 ;; show subviews
                 "links" "maps" "paths" "summary"
                 ;; assertion command heads ('assert ok CMD',
                 ;; 'assert fail CMD'). 'not' also appears here as
                 ;; the optional leading negation ('assert not ok
                 ;; CMD'). "true" and "false" are bare boolean
                 ;; literals, not predicates -- they get default
                 ;; face like other literals.
                 "fail" "not" "ok"
                 ;; named predicates registered as pure builtins
                 ;; (shell/syntax/purebuiltin.go). 'not-empty' is
                 ;; the expression-position unary predicate;
                 ;; 'path-exists' is the file-system predicate
                 ;; (renamed from the older 'path' verb).
                 "contains" "empty" "missing" "not-empty"
                 "null" "path-exists" "present"
                 ;; logical operators
                 "and" "or"
                 ;; foreach separator
                 "in"
                 ;; poll clause keywords (recognised only
                 ;; between 'poll' and the block body)
                 "timeout" "every" "unless"
                 ;; matches expression operator and its optional
                 ;; 'exhaustive' modifier
                 "matches" "exhaustive"
                 ;; record literal constructor (`record { name:
                 ;; value ... }'); fontified like matches when it
                 ;; appears in argument position
                 "record"
                 ;; comparison operators (semantics chosen by
                 ;; operand type: number-vs-number numeric,
                 ;; string-vs-string textual, bool-vs-bool only
                 ;; supports == and !=, cross-type errors)
                 "==" "!=" "<" "<=" ">" ">="
                 ;; arithmetic operators (binary '+', '-', '*',
                 ;; '/', '%'; unary '-' reuses the same token).
                 ;; '-' appears here too: the line tokeniser only
                 ;; emits a bare '-' when it is not part of a flag
                 ;; ('-x', '--long'), so a standalone dash in
                 ;; expression position fontifies as an operator.
                 "+" "-" "*" "/" "%"))
      (puthash w t ht))
    ht)
  "Hash table of bpfman subcommands, attach types, subviews, and operators.")

;; ---- Token kind constants ----

(defconst bpfman--tok-word 0)
(defconst bpfman--tok-varref 1)
(defconst bpfman--tok-flag 2)
(defconst bpfman--tok-assign 3)
(defconst bpfman--tok-string 4)
(defconst bpfman--tok-select 5)
(defconst bpfman--tok-adapter-ref 6)
(defconst bpfman--tok-delim 7)   ; { } ; |> <- -- resets state to command
(defconst bpfman--tok-block 8)   ; { } -- same role but block-scoped

(defconst bpfman--adapter-prefixes '("file")
  "Known adapter prefixes for inline file:$var syntax.")

(defun bpfman--ident-start-char-p (ch)
  "Return non-nil when CH can start a bpfman identifier."
  (and ch
       (or (and (>= ch ?a) (<= ch ?z))
           (and (>= ch ?A) (<= ch ?Z))
           (= ch ?_))))

(defun bpfman--scan-braced-varref-body (pos eol)
  "Return the position after a braced varref body starting at POS.
POS must point just after the opening brace. The scan is
mid-edit tolerant: an unterminated braced reference consumes to
EOL, matching the old tokeniser behaviour."
  (while (and (< pos eol) (/= (char-after pos) ?}))
    (setq pos (1+ pos)))
  (if (< pos eol)
      (1+ pos)
    pos))

(defun bpfman--scan-bare-varref-body (pos eol)
  "Return the end of a bare varref body starting at POS, or nil.
The accepted shape is IDENT followed by any number of `.field'
or `[n]' path segments. The scanner deliberately stays tolerant
of incomplete field/index segments so mid-edit highlighting does
not disappear."
  (when (and (< pos eol)
             (bpfman--ident-start-char-p (char-after pos)))
    (save-excursion
      (goto-char pos)
      (skip-chars-forward "a-zA-Z0-9_" eol)
      (while (and (< (point) eol)
                  (let ((ch (char-after (point))))
                    (or (= ch ?.) (= ch ?\[))))
        (if (= (char-after (point)) ?.)
            (progn
              (forward-char 1)
              (skip-chars-forward "a-zA-Z0-9_" eol))
          (forward-char 1)
          (skip-chars-forward "0-9" eol)
          (when (and (< (point) eol)
                     (= (char-after (point)) ?\]))
            (forward-char 1))))
      (point))))

(defun bpfman--scan-varref-after-dollar (pos eol)
  "Return the end of a varref whose '$' has already been consumed.
POS points at either the opening brace of `${...}' or the first
character of a bare reference body."
  (cond
   ((and (< pos eol) (= (char-after pos) ?{))
    (bpfman--scan-braced-varref-body (1+ pos) eol))
   (t
    (bpfman--scan-bare-varref-body pos eol))))

(defun bpfman--scan-quoted-string (pos eol quote-char)
  "Return the end of a quoted string starting at POS.
QUOTE-CHAR is either a single or double quote. Double-quoted
strings honour backslash escapes while scanning so an escaped
quote does not terminate the token."
  (setq pos (1+ pos))
  (while (and (< pos eol)
              (/= (char-after pos) quote-char))
    (if (and (= quote-char ?\")
             (= (char-after pos) ?\\)
             (< (1+ pos) eol))
        (setq pos (+ pos 2))
      (setq pos (1+ pos))))
  (if (< pos eol)
      (1+ pos)
    pos))

(defun bpfman--two-char-token-p (pos eol first second)
  "Return non-nil when POS starts FIRST SECOND before EOL."
  (and (< (1+ pos) eol)
       (= (char-after pos) first)
       (= (char-after (1+ pos)) second)))

(defun bpfman--scan-flag-token (pos eol)
  "Return the end of a flag token starting at POS."
  (save-excursion
    (goto-char (1+ pos))
    (if (and (< (point) eol) (= (char-after (point)) ?-))
        (progn
          (forward-char 1)
          (skip-chars-forward "a-zA-Z0-9" eol)
          (while (and (< (point) eol)
                      (= (char-after (point)) ?-)
                      (< (1+ (point)) eol)
                      (let ((ch (char-after (1+ (point)))))
                        (or (and (>= ch ?a) (<= ch ?z))
                            (and (>= ch ?A) (<= ch ?Z))
                            (and (>= ch ?0) (<= ch ?9)))))
            (forward-char 1)
            (skip-chars-forward "a-zA-Z0-9" eol)))
      (skip-chars-forward "a-zA-Z" eol))
    (point)))

(defun bpfman--scan-plain-word (pos eol)
  "Return the end of a plain word token starting at POS."
  (save-excursion
    (goto-char pos)
    (skip-chars-forward "^ \t\n#'\"$[](){}|;" eol)
    (point)))

(defun bpfman--adapter-prefix-p (text)
  "Return non-nil when TEXT is an inline adapter prefix."
  (seq-some (lambda (prefix)
              (string= text (concat prefix ":")))
            bpfman--adapter-prefixes))

(defun bpfman--scan-adapter-ref-end (dollar-pos eol)
  "Return the end of an adapter reference whose '$' starts at DOLLAR-POS.
Invalid or incomplete references consume just the '$', preserving
the tokeniser's old mid-edit behaviour for `file:$'."
  (or (bpfman--scan-varref-after-dollar (1+ dollar-pos) eol)
      (1+ dollar-pos)))

(defun bpfman--looking-at-line-continuation-p (pos eol)
  "Return non-nil when POS starts a backslash line continuation."
  (and (= (char-after pos) ?\\)
       (< (1+ pos) eol)
       (or (= (char-after (1+ pos)) ?\n)
           (and (= (char-after (1+ pos)) ?\r)
                (< (+ pos 2) eol)
                (= (char-after (+ pos 2)) ?\n)))))

(defun bpfman--skip-line-continuation (pos)
  "Return the position after the line continuation at POS."
  (+ pos (if (= (char-after (1+ pos)) ?\n) 2 3)))

(defun bpfman--looking-at-comment-p (pos)
  "Return non-nil when POS starts a comment."
  (= (char-after pos) ?#))

(defun bpfman--looking-at-quoted-string-p (pos)
  "Return non-nil when POS starts a quoted string."
  (let ((ch (char-after pos)))
    (or (= ch ?\") (= ch ?'))))

(defun bpfman--looking-at-thread-p (pos eol)
  "Return non-nil when POS starts a thread operator."
  (bpfman--two-char-token-p pos eol ?| ?>))

(defun bpfman--looking-at-bind-p (pos eol)
  "Return non-nil when POS starts a bind sigil."
  (bpfman--two-char-token-p pos eol ?< ?-))

(defun bpfman--delimiter-char-p (ch)
  "Return non-nil when CH is a structural delimiter."
  (or (= ch ?{) (= ch ?})
      (= ch ?\() (= ch ?\))
      (= ch ?\;)))

(defun bpfman--ignored-bracket-char-p (ch)
  "Return non-nil when CH is a bracket skipped by the highlighter."
  (or (= ch ?\[) (= ch ?\])))

(defun bpfman--looking-at-varref-p (pos)
  "Return non-nil when POS starts a variable reference."
  (= (char-after pos) ?$))

(defun bpfman--looking-at-equals-p (pos)
  "Return non-nil when POS starts an equals token."
  (= (char-after pos) ?=))

(defun bpfman--looking-at-flag-p (pos eol)
  "Return non-nil when POS starts a short or long flag."
  (and (= (char-after pos) ?-)
       (< (1+ pos) eol)
       (let ((next (char-after (1+ pos))))
         (or (= next ?-)
             (and (>= next ?a) (<= next ?z))
             (and (>= next ?A) (<= next ?Z))))))

(defun bpfman--token (kind beg end)
  "Return a token of KIND spanning BEG to END."
  (list kind beg end))

(defun bpfman--scan-result (token next-pos)
  "Return a token scan result carrying TOKEN and NEXT-POS."
  (list :token token :next next-pos))

(defun bpfman--skip-result (next-pos)
  "Return a token scan result that advances to NEXT-POS without a token."
  (list :next next-pos))

(defun bpfman--stop-result ()
  "Return a token scan result that stops tokenising the line."
  (list :stop t))

(defun bpfman--scan-result-token (result)
  "Return RESULT's token, or nil."
  (plist-get result :token))

(defun bpfman--scan-result-next (result)
  "Return RESULT's next position, or nil."
  (plist-get result :next))

(defun bpfman--scan-result-stop-p (result)
  "Return non-nil when RESULT stops tokenising."
  (plist-get result :stop))

(defun bpfman--scan-varref-token-at (pos eol)
  "Return a scan result for a variable reference at POS."
  (let ((end (bpfman--scan-varref-after-dollar (1+ pos) eol)))
    (if end
        (bpfman--scan-result (bpfman--token bpfman--tok-varref pos end) end)
      (bpfman--skip-result (1+ pos)))))

(defun bpfman--scan-equals-token-at (pos eol)
  "Return a scan result for '=' or '==' at POS."
  (if (and (< (1+ pos) eol) (= (char-after (1+ pos)) ?=))
      (bpfman--scan-result
       (bpfman--token bpfman--tok-word pos (+ pos 2))
       (+ pos 2))
    (bpfman--scan-result
     (bpfman--token bpfman--tok-assign pos (1+ pos))
     (1+ pos))))

(defun bpfman--scan-word-or-adapter-token-at (pos eol)
  "Return a scan result for a word or adapter reference at POS."
  (let* ((start pos)
         (word-end (bpfman--scan-plain-word pos eol)))
    (if (= word-end start)
        (bpfman--skip-result (1+ pos))
      (let ((text (buffer-substring-no-properties start word-end)))
        (if (and (< word-end eol)
                 (= (char-after word-end) ?$)
                 (bpfman--adapter-prefix-p text))
            (let ((end (bpfman--scan-adapter-ref-end word-end eol)))
              (bpfman--scan-result
               (bpfman--token bpfman--tok-adapter-ref start end)
               end))
          (bpfman--scan-result
           (bpfman--token (if (string= text "select")
                              bpfman--tok-select
                            bpfman--tok-word)
                          start word-end)
           word-end))))))

(defun bpfman--scan-token-at (pos eol)
  "Return the token scan result for POS in the current buffer."
  (let ((ch (char-after pos)))
    (cond
     ((bpfman--looking-at-line-continuation-p pos eol)
      (bpfman--skip-result (bpfman--skip-line-continuation pos)))
     ((bpfman--looking-at-comment-p pos)
      (bpfman--stop-result))
     ((bpfman--looking-at-quoted-string-p pos)
      (let ((end (bpfman--scan-quoted-string pos eol ch)))
        (bpfman--scan-result (bpfman--token bpfman--tok-string pos end) end)))
     ((bpfman--looking-at-thread-p pos eol)
      (bpfman--scan-result
       (bpfman--token bpfman--tok-delim pos (+ pos 2))
       (+ pos 2)))
     ((bpfman--looking-at-bind-p pos eol)
      (bpfman--scan-result
       (bpfman--token bpfman--tok-delim pos (+ pos 2))
       (+ pos 2)))
     ((bpfman--delimiter-char-p ch)
      (bpfman--scan-result
       (bpfman--token bpfman--tok-delim pos (1+ pos))
       (1+ pos)))
     ((bpfman--ignored-bracket-char-p ch)
      (bpfman--skip-result (1+ pos)))
     ((bpfman--looking-at-varref-p pos)
      (bpfman--scan-varref-token-at pos eol))
     ((bpfman--looking-at-equals-p pos)
      (bpfman--scan-equals-token-at pos eol))
     ((bpfman--looking-at-flag-p pos eol)
      (let ((end (bpfman--scan-flag-token pos eol)))
        (bpfman--scan-result (bpfman--token bpfman--tok-flag pos end) end)))
     (t
      (bpfman--scan-word-or-adapter-token-at pos eol)))))

;; ---- Line tokeniser ----

(defun bpfman--tokenise-line (bol eol)
  "Tokenise the buffer region from BOL to EOL.
Return a list of (KIND BEG END) triples.  Stops at an unquoted #."
  (let ((pos bol)
        tokens)
    (catch 'done
      (while (< pos eol)
        ;; Skip whitespace, including the newline at a backslash-
        ;; continuation boundary. The fontifier passes a logical-
        ;; line range that may span several physical lines glued
        ;; by '\\\n'; treating the embedded newlines as whitespace
        ;; lets the state machine carry flag and argument context
        ;; across the continuation.
        (goto-char pos)
        (skip-chars-forward " \t\r\n" eol)
        (setq pos (point))
        (when (>= pos eol) (throw 'done nil))
        (let* ((result (bpfman--scan-token-at pos eol))
               (token (bpfman--scan-result-token result)))
          (when (bpfman--scan-result-stop-p result)
            (throw 'done nil))
          (when token
            (push token tokens))
          (setq pos (or (bpfman--scan-result-next result)
                        (1+ pos))))))
    (nreverse tokens)))

;; ---- Structural font-lock ----

(defun bpfman--double-quoted-string-token-p (beg end)
  "Return non-nil when [BEG, END) is a double-quoted string token."
  (and (> end beg) (= (char-after beg) ?\")))

(defun bpfman--string-escape-at-p (pos end)
  "Return non-nil when POS starts a string escape before END."
  (and (= (char-after pos) ?\\) (< (1+ pos) end)))

(defun bpfman--interp-start-at-p (pos end)
  "Return non-nil when POS starts a double-quoted interpolation."
  (and (= (char-after pos) ?$)
       (< (1+ pos) end)
       (= (char-after (1+ pos)) ?{)))

(defun bpfman--fontify-string-literal (beg end)
  "Apply string face to the literal span [BEG, END)."
  (when (< beg end)
    (put-text-property beg end 'face 'font-lock-string-face)))

(defun bpfman--fontify-interp-delimiter (beg end)
  "Apply keyword face to interpolation delimiter [BEG, END)."
  (put-text-property beg end 'face 'font-lock-keyword-face))

(defun bpfman--fontify-interp-body (beg end)
  "Apply variable-name face to interpolation body [BEG, END)."
  (when (< beg end)
    (put-text-property beg end 'face 'font-lock-variable-name-face)))

(defun bpfman--scan-interp-body-end (body-start end)
  "Return the closing brace position for interpolation BODY-START.
Return nil when the interpolation is unterminated before END.
Nested braces are counted so `${$x |> jq \"{...}\"}'-shaped
edits do not close at the first inner brace."
  (let ((pos body-start)
        (depth 1))
    (while (and (< pos end) (> depth 0))
      (let ((ch (char-after pos)))
        (cond
         ((= ch ?{) (setq depth (1+ depth)))
         ((= ch ?}) (setq depth (1- depth)))))
      (unless (= depth 0)
        (setq pos (1+ pos))))
    (and (= depth 0) pos)))

(defun bpfman--interp-string-segments (beg end)
  "Return semantic segments for string token [BEG, END).
Each segment is (ROLE BEG END). Double-quoted strings split
interpolation delimiters and bodies from literal runs; other
string tokens are one literal segment."
  (if (not (bpfman--double-quoted-string-token-p beg end))
      (list (list 'string beg end))
    (let ((pos beg)
          (lit-start beg)
          segments)
      (while (< pos end)
        (cond
         ((bpfman--string-escape-at-p pos end)
          (setq pos (+ pos 2)))
         ((bpfman--interp-start-at-p pos end)
          (let ((body-end (bpfman--scan-interp-body-end (+ pos 2) end)))
            (if body-end
                (let ((body-start (+ pos 2))
                      (next-lit-start (1+ body-end)))
                  (when (< lit-start pos)
                    (push (list 'string lit-start pos) segments))
                  (push (list 'interp-delim pos body-start) segments)
                  (when (< body-start body-end)
                    (push (list 'interp-body body-start body-end) segments))
                  (push (list 'interp-delim body-end next-lit-start) segments)
                  (setq lit-start next-lit-start
                        pos next-lit-start))
              (push (list 'string pos end) segments)
              (setq lit-start end
                    pos end))))
         (t
          (setq pos (1+ pos)))))
      (when (< lit-start end)
        (push (list 'string lit-start end) segments))
      (nreverse segments))))

(defun bpfman--fontify-interp-segment (segment)
  "Apply font-lock face for one interpolation SEGMENT."
  (pcase-let ((`(,role ,beg ,end) segment))
    (pcase role
      ('string
       (bpfman--fontify-string-literal beg end))
      ('interp-delim
       (bpfman--fontify-interp-delimiter beg end))
      ('interp-body
       (bpfman--fontify-interp-body beg end)))))

(defun bpfman--fontify-interp-string (beg end)
  "Fontify a string token in [BEG, END) with interpolation awareness."
  (dolist (segment (bpfman--interp-string-segments beg end))
    (bpfman--fontify-interp-segment segment)))

(defun bpfman--role (kind beg end)
  "Return a classified role KIND spanning BEG to END."
  (list kind beg end))

(defun bpfman--classification-result (state &rest roles)
  "Return a classifier result with next STATE and ROLES."
  (list :state state :roles (delq nil roles)))

(defun bpfman--classification-state (result)
  "Return RESULT's next classifier state."
  (plist-get result :state))

(defun bpfman--classification-roles (result)
  "Return RESULT's emitted roles."
  (plist-get result :roles))

(defun bpfman--keyword-role (beg end)
  "Return a keyword role spanning BEG to END."
  (bpfman--role 'keyword beg end))

(defun bpfman--variable-role (beg end)
  "Return a variable role spanning BEG to END."
  (bpfman--role 'variable beg end))

(defun bpfman--constant-role (beg end)
  "Return a constant role spanning BEG to END."
  (bpfman--role 'constant beg end))

(defun bpfman--builtin-role (beg end)
  "Return a builtin role spanning BEG to END."
  (bpfman--role 'builtin beg end))

(defun bpfman--string-role (beg end)
  "Return a string role spanning BEG to END."
  (bpfman--role 'string beg end))

(defun bpfman--ident-role (text beg end)
  "Return a variable role for TEXT in [BEG, END), or nil."
  (and (bpfman--ident-p text)
       (bpfman--variable-role beg end)))

(defun bpfman--identifier-prefix-role (beg end)
  "Return a variable role for the identifier prefix in [BEG, END)."
  (let ((ident-end (bpfman--identifier-prefix-end beg end)))
    (and ident-end
         (bpfman--variable-role beg ident-end))))

(defun bpfman--parser-keyword-p (text)
  "Return non-nil when TEXT is a parser-routed statement keyword."
  (member text '("defer" "if" "elif" "else" "poll" "retry"
                 "return" "break" "continue" "assert" "require")))

(defun bpfman--classify-start-word (text beg end)
  "Classify a word TEXT at statement-start state."
  (cond
   ((string= text "let")
    (bpfman--classification-result 'let-name (bpfman--keyword-role beg end)))
   ((string= text "guard")
    (bpfman--classification-result 'let-name (bpfman--keyword-role beg end)))
   ((string= text "foreach")
    (bpfman--classification-result 'foreach-name (bpfman--keyword-role beg end)))
   ((string= text "def")
    (bpfman--classification-result 'def-name (bpfman--keyword-role beg end)))
   ((string= text "record")
    ;; A record literal in binding-RHS position (`let r = record
    ;; { ... }'). Fontify the keyword as a builtin, matching the
    ;; sibling `matches' brace-block constructor; the `{' that
    ;; follows resets to start state so the block body classifies
    ;; normally. In argument position (after a verb) the args-state
    ;; branch catches `record' via bpfman--subcommands instead.
    (bpfman--classification-result 'args (bpfman--builtin-role beg end)))
   ((bpfman--parser-keyword-p text)
    (bpfman--classification-result
     (if (string= text "defer") 'start 'args)
     (bpfman--keyword-role beg end)))
   (t
    (bpfman--classification-result
     'subcommand
     (and (gethash text bpfman--commands)
          (bpfman--keyword-role beg end))))))

(defun bpfman--classify-delimiter-token (state beg end)
  "Classify delimiter token [BEG, END) while in STATE."
  (pcase (char-after beg)
    ((or ?| ?<)
     (bpfman--classification-result 'start (bpfman--keyword-role beg end)))
    (?\(
     (bpfman--classification-result
      (cond
       ((eq state 'def-params) 'def-params-list)
       ((eq state 'let-name) 'tuple-targets-let)
       ((eq state 'foreach-name) 'tuple-targets-foreach)
       (t state))))
    (?\)
     (bpfman--classification-result
      (cond
       ((memq state '(def-params-list def-param-type)) 'start)
       ((eq state 'tuple-targets-let) 'let-eq)
       ((eq state 'tuple-targets-foreach) 'args)
       (t state))))
    ((or ?{ ?} ?\;)
     (bpfman--classification-result 'start))
    (_
     (bpfman--classification-result state))))

(defun bpfman--classify-word-token (state text beg end)
  "Classify word TEXT in STATE."
  (pcase state
    ('start
     (bpfman--classify-start-word text beg end))
    ('let-name
     (bpfman--classification-result 'let-eq (bpfman--ident-role text beg end)))
    ('foreach-name
     (bpfman--classification-result 'args (bpfman--ident-role text beg end)))
    ('def-name
     (bpfman--classification-result 'def-params (bpfman--ident-role text beg end)))
    ('def-params-list
     ;; A def parameter may carry a type annotation: `name: type'.
     ;; The colon glues to the name in tokenisation, so a token
     ;; ending in `:' is an annotated parameter whose type word
     ;; follows. The name keeps its variable role; the next token
     ;; is classified in `def-param-type'.
     (bpfman--classification-result
      (if (string-suffix-p ":" text) 'def-param-type 'def-params-list)
      (bpfman--identifier-prefix-role beg end)))
    ('def-param-type
     ;; Type position after `name:'. Highlight the accepted
     ;; annotation types as a type; anything else is a malformed
     ;; annotation the parser rejects, left as an ordinary
     ;; identifier. Either way the list continues.
     (bpfman--classification-result
      'def-params-list
      (if (member text bpfman--def-param-types)
          (list 'type beg end)
        (bpfman--identifier-prefix-role beg end))))
    ((or 'tuple-targets-let 'tuple-targets-foreach)
     (bpfman--classification-result state (bpfman--identifier-prefix-role beg end)))
    ('let-eq
     (bpfman--classification-result 'args))
    ('subcommand
     (bpfman--classification-result
      'args
      (and (gethash text bpfman--subcommands)
           (bpfman--builtin-role beg end))))
    ('args
     (bpfman--classification-result
      'args
      (and (gethash text bpfman--subcommands)
           (bpfman--builtin-role beg end))))
    (_
     (bpfman--classification-result state))))

(defun bpfman--classify-token-in-state (state tok)
  "Classify TOK while in STATE."
  (let* ((kind (nth 0 tok))
         (beg (nth 1 tok))
         (end (nth 2 tok))
         (text (buffer-substring-no-properties beg end)))
    (cond
     ((= kind bpfman--tok-string)
      (bpfman--classification-result state (bpfman--string-role beg end)))
     ((or (= kind bpfman--tok-varref)
          (= kind bpfman--tok-adapter-ref))
      (bpfman--classification-result
       (if (eq state 'start) 'args state)
       (bpfman--variable-role beg end)))
     ((= kind bpfman--tok-flag)
      (bpfman--classification-result
       (if (eq state 'start) 'args state)
       (bpfman--constant-role beg end)))
     ((= kind bpfman--tok-select)
      (bpfman--classification-result 'args (bpfman--keyword-role beg end)))
     ((= kind bpfman--tok-delim)
      (bpfman--classify-delimiter-token state beg end))
     ((= kind bpfman--tok-assign)
      (if (eq state 'let-eq)
          (bpfman--classification-result 'start (bpfman--keyword-role beg end))
        (bpfman--classification-result 'args)))
     ((= kind bpfman--tok-word)
      (bpfman--classify-word-token state text beg end))
     (t
      (bpfman--classification-result state)))))

(defun bpfman--classify-line-tokens (tokens)
  "Return semantic highlight roles for TOKENS.
Each role is a list (ROLE BEG END). TOKENS is a list of
(KIND BEG END) as returned by `bpfman--tokenise-line'. The
classifier mirrors the parser's statement-routing states closely
enough for editor feedback while still tolerating incomplete
mid-edit lines."
  (let ((state 'start)
        roles)
    (dolist (tok tokens)
      (let ((result (bpfman--classify-token-in-state state tok)))
        (setq roles (append (reverse (bpfman--classification-roles result))
                            roles)
              state (bpfman--classification-state result))))
    (nreverse roles)))

(defun bpfman--identifier-prefix-end (beg end)
  "Return end of the identifier prefix in [BEG, END), or nil."
  (save-excursion
    (goto-char beg)
    (skip-chars-forward "a-zA-Z0-9_" end)
    (let ((ident-end (point)))
      (and (> ident-end beg)
           (bpfman--ident-p
            (buffer-substring-no-properties beg ident-end))
           ident-end))))

(defun bpfman--fontify-line-tokens (tokens)
  "Apply faces to TOKENS based on their classified structural role."
  (dolist (role (bpfman--classify-line-tokens tokens))
    (bpfman--apply-role-face role)))

(defun bpfman--role-face (kind)
  "Return the font-lock face for semantic role KIND, or nil."
  (pcase kind
    ('keyword 'font-lock-keyword-face)
    ('variable 'font-lock-variable-name-face)
    ('constant 'font-lock-constant-face)
    ('builtin 'font-lock-builtin-face)
    ('type 'font-lock-type-face)
    (_ nil)))

(defun bpfman--apply-role-face (role)
  "Apply the face described by semantic ROLE."
  (pcase-let ((`(,kind ,beg ,end) role))
    (if (eq kind 'string)
        (bpfman--fontify-interp-string beg end)
      (let ((face (bpfman--role-face kind)))
        (when face
          (put-text-property beg end 'face face))))))

(defun bpfman--ident-p (str)
  "Return non-nil if STR is a valid bpfman identifier."
  (and (> (length str) 0)
       (string-match-p "\\`[a-zA-Z_][a-zA-Z0-9_]*\\'" str)))

(defun bpfman--logical-line-end (bol)
  "Return the buffer position of the end of the logical line starting at BOL.
A logical line is a physical line plus any following physical
lines joined by an unescaped trailing backslash, with quote
state tracked across the boundary so a backslash inside a
single-quoted string does not look like a continuation. Comments
truncate the line for the continuation check; a '#' inside
quotes is literal text and does not end the line."
  (save-excursion
    (goto-char bol)
    (let ((in-single nil)
          (in-double nil))
      (catch 'done
        (while t
          (let ((eol (line-end-position))
                (last-non-space nil))
            (while (< (point) eol)
              (let ((ch (char-after)))
                (cond
                 ;; In a double-quoted string, '\\X' is an escape
                 ;; pair: the runtime decodes \n, \t, \", \$, etc.
                 ;; Outside strings or in a single-quoted span, a
                 ;; bare '\\' is just one character (handled by the
                 ;; default branch below).
                 ((and (= ch ?\\) in-double (< (1+ (point)) eol))
                  (forward-char 2)
                  (setq last-non-space (1- (point))))
                 ((and (= ch ?\') (not in-double))
                  (setq in-single (not in-single))
                  (forward-char 1)
                  (setq last-non-space (1- (point))))
                 ((and (= ch ?\") (not in-single))
                  (setq in-double (not in-double))
                  (forward-char 1)
                  (setq last-non-space (1- (point))))
                 ((and (= ch ?#) (not in-single) (not in-double))
                  ;; A trailing comment cannot host a continuation:
                  ;; a backslash in the comment body is literal.
                  ;; Skip to EOL without recording it as
                  ;; non-whitespace.
                  (goto-char eol))
                 ((or (= ch ?\s) (= ch ?\t) (= ch ?\r))
                  (forward-char 1))
                 (t
                  (forward-char 1)
                  (setq last-non-space (1- (point)))))))
            (if (and last-non-space
                     (= (char-after last-non-space) ?\\)
                     (not in-single)
                     (not in-double)
                     (< eol (point-max)))
                (progn
                  (forward-char 1)
                  ;; Loop re-assesses next physical line.
                  )
              (throw 'done eol))))))))

(defun bpfman--fontify-region (beg end)
  "Fontify the buffer region from BEG to END one logical line at a time.
Backslash-continued lines fontify as one logical line so the
state machine carries flag/argument context across the
continuation; multi-line guard or load statements highlight
the same way they read."
  (save-excursion
    (goto-char beg)
    (beginning-of-line)
    (while (< (point) end)
      (let* ((bol (line-beginning-position))
             (lol (bpfman--logical-line-end bol)))
        (bpfman--fontify-line-tokens
         (bpfman--tokenise-line bol lol))
        (goto-char lol)
        (forward-line 1)))))

;; ---- Formatting ----

(defcustom bpfman-shell-program "bpfman-shell"
  "Executable used to format bpfman-shell buffers.
Invoked as `bpfman-shell --fmt -', reading the buffer on stdin
and emitting canonical source on stdout. Set this to an absolute
path (for example bin/bpfman-shell, via a per-project
.dir-locals.el) when the binary is not on `exec-path'."
  :type 'string
  :group 'bpfman)

(defcustom bpfman-format-show-errors t
  "When non-nil, pop up a buffer with the diagnostics on a format failure."
  :type 'boolean
  :group 'bpfman)

(defcustom bpfman-format-on-save nil
  "When non-nil, format the buffer with `bpfman-format-buffer' before saving."
  :type 'boolean
  :safe #'booleanp
  :group 'bpfman)

(defcustom bpfman-indent-offset 4
  "Basic indentation width for bpfman-shell blocks.
This is only for typing ergonomics; `bpfman-shell --fmt' remains
the canonical source of layout."
  :type 'integer
  :safe #'integerp
  :group 'bpfman)

(defun bpfman--indentation-plan ()
  "Return the indentation plan for the current line."
  (let* ((bol (line-beginning-position))
         (continuation (bpfman--continuation-indentation bol))
         (block-indent (bpfman--block-indentation bol)))
    (list :indent (or continuation block-indent))))

(defun bpfman--block-indentation (bol)
  "Return shallow brace-based indentation for the line at BOL."
  (let* ((depth (bpfman--brace-depth-before bol))
         (line-depth (if (bpfman--line-starts-with-closing-brace-p bol)
                         (max 0 (1- depth))
                       depth)))
    (* line-depth bpfman-indent-offset)))

(defun bpfman--line-starts-with-closing-brace-p (bol)
  "Return non-nil when the line at BOL starts with a closing brace."
  (save-excursion
    (goto-char bol)
    (back-to-indentation)
    (eq (char-after) ?})))

(defun bpfman--brace-depth-before (pos)
  "Return unmatched brace depth before POS.
Only braces outside strings and comments count. Parentheses and
brackets are deliberately ignored; indentation is a light typing
aid, not a formatter."
  (save-excursion
    (let ((depth 0))
      (goto-char (point-min))
      (while (< (point) pos)
        (let ((ch (char-after))
              (ppss (syntax-ppss)))
          (cond
           ((or (nth 3 ppss) (nth 4 ppss))
            (forward-char 1))
           ((= ch ?{)
            (setq depth (1+ depth))
            (forward-char 1))
           ((= ch ?})
            (setq depth (max 0 (1- depth)))
            (forward-char 1))
           (t
            (forward-char 1)))))
      depth)))

(defun bpfman--line-continues-p (bol)
  "Return non-nil when the physical line at BOL continues with '\\'."
  (save-excursion
    (goto-char bol)
    (> (bpfman--logical-line-end bol) (line-end-position))))

(defun bpfman--continuation-start-bol (bol)
  "Return the first line in BOL's continuation chain, or nil."
  (save-excursion
    (goto-char bol)
    (let ((start nil)
          (done nil))
      (while (not done)
        (if (and (= (forward-line -1) 0)
                 (bpfman--line-continues-p (line-beginning-position)))
            (setq start (line-beginning-position))
          (setq done t)))
      start)))

(defun bpfman--continuation-indentation (bol)
  "Return indentation for a continuation line at BOL, or nil."
  (let ((start (bpfman--continuation-start-bol bol)))
    (when start
      (save-excursion
        (goto-char start)
        (+ (max (current-indentation)
                (bpfman--block-indentation start))
           bpfman-indent-offset)))))

(defun bpfman-indent-line ()
  "Indent the current line for comfortable editing.
This intentionally implements only shallow typing rules. The
formatter remains the authority for canonical layout."
  (interactive)
  (let* ((plan (bpfman--indentation-plan))
         (indent (plist-get plan :indent))
         (point-in-indent (<= (current-column) (current-indentation))))
    (indent-line-to indent)
    (when point-in-indent
      (back-to-indentation))))

(defun bpfman--replace-buffer-from (source)
  "Replace the current buffer's contents with those of buffer SOURCE.
Use `replace-buffer-contents' when available so point and markers
survive the rewrite; otherwise fall back to erase-and-insert."
  (if (fboundp 'replace-buffer-contents)
      (replace-buffer-contents source)
    (let ((point (point)))
      (erase-buffer)
      (insert-buffer-substring source)
      (goto-char (min point (point-max))))))

(defun bpfman--report-format-error (errfile status)
  "Report a format failure with exit STATUS and diagnostics in ERRFILE."
  (let ((msg (with-temp-buffer
               (ignore-errors (insert-file-contents errfile))
               (string-trim (buffer-string)))))
    (when (and bpfman-format-show-errors (not (string-empty-p msg)))
      (with-current-buffer (get-buffer-create "*bpfman-shell fmt*")
        (let ((inhibit-read-only t))
          (erase-buffer)
          (insert msg "\n"))
        (goto-char (point-min))
        (special-mode)
        (display-buffer (current-buffer))))
    (message "bpfman-shell --fmt failed (exit %s)%s"
             status
             (if (string-empty-p msg)
                 ""
               (concat ": " (car (split-string msg "\n")))))))

(defun bpfman--format-query ()
  "Return the `bpfman-shell --fmt' query for the current buffer."
  (list :input 'stdin :args '("--fmt" "-")))

(defun bpfman--run-format-query (query outbuf errfile)
  "Run format QUERY, writing stdout to OUTBUF and stderr to ERRFILE."
  (condition-case err
      (pcase (plist-get query :input)
        ('stdin
         (apply #'call-process-region
                (point-min) (point-max)
                bpfman-shell-program nil (list outbuf errfile) nil
                (plist-get query :args))))
    (file-missing
     (user-error "Cannot run %S: %s"
                 bpfman-shell-program
                 (error-message-string err)))))

(defun bpfman-format-buffer ()
  "Format the current buffer as canonical bpfman-shell source.
Pipe the buffer through `bpfman-shell --fmt -' (see
`bpfman-shell-program'). On success replace the buffer with the
formatted result, preserving point where possible. On a parse or
formatter error leave the buffer untouched; the formatter emits
nothing on stdout in that case, so a broken edit can never
clobber the buffer. When `bpfman-format-show-errors' is non-nil
the diagnostics are shown."
  (interactive)
  (let ((outbuf (generate-new-buffer " *bpfman-fmt-out*"))
        (errfile (make-temp-file "bpfman-fmt-err"))
        (query (bpfman--format-query))
        (status nil))
    (unwind-protect
        (progn
          (setq status (bpfman--run-format-query query outbuf errfile))
          (if (and (integerp status) (= status 0))
              (bpfman--replace-buffer-from outbuf)
            (bpfman--report-format-error errfile status)))
      (kill-buffer outbuf)
      (ignore-errors (delete-file errfile)))))

(defun bpfman--maybe-format-before-save ()
  "Format the buffer before saving when `bpfman-format-on-save' is set."
  (when bpfman-format-on-save
    (bpfman-format-buffer)))

;; ---- Xref ----

(defun bpfman--xref-backend ()
  "Return the bpfman xref backend for `xref-backend-functions'."
  'bpfman)

(cl-defmethod xref-backend-identifier-at-point ((_backend (eql bpfman)))
  "Return the bpfman identifier at point, or nil."
  (bpfman--identifier-at-point))

(cl-defmethod xref-backend-definitions ((_backend (eql bpfman)) identifier)
  "Return xref definitions for IDENTIFIER using `bpfman-shell --symbols'."
  (let* ((doc (bpfman--symbols-document))
         (symbols (alist-get 'symbols doc))
         (query-file (alist-get 'file doc))
         (line (line-number-at-pos))
         (col (1+ (current-column)))
         (match (bpfman--resolve-symbol identifier symbols query-file line col)))
    (when match
      (list (bpfman--xref-definition identifier match query-file)))))

(defun bpfman--identifier-at-point ()
  "Return the bpfman identifier under point, stripped of path syntax.
Wrapped in `save-excursion' because `bpfman--tokenise-line' moves
point, and xref calls this immediately before
`xref-backend-definitions', which reads point to locate the
reference; leaving point moved resolves against the wrong scope."
  (save-excursion
    (let* ((bol (line-beginning-position))
           (eol (bpfman--logical-line-end bol))
           (pt (point))
           (tokens (bpfman--tokenise-line bol eol))
           ident)
      (while (and tokens (not ident))
        (let* ((tok (car tokens))
               (kind (nth 0 tok))
               (beg (nth 1 tok))
               (end (nth 2 tok)))
          (when (and (<= beg pt) (<= pt end))
            (setq ident
                  (bpfman--identifier-from-token
                   kind
                   (buffer-substring-no-properties beg end)))))
        (setq tokens (cdr tokens)))
      ident)))

(defun bpfman--identifier-from-token (kind text)
  "Extract a bpfman identifier from token KIND and TEXT."
  (cond
   ((or (= kind bpfman--tok-varref)
        (= kind bpfman--tok-adapter-ref))
    (when (string-match "\\$[{]?\\([A-Za-z_][A-Za-z0-9_]*\\)" text)
      (match-string 1 text)))
   ((= kind bpfman--tok-word)
    (and (bpfman--ident-p text) text))))

(defun bpfman--symbols-document ()
  "Return the JSON document emitted by `bpfman-shell --symbols'."
  (let ((outbuf (generate-new-buffer " *bpfman-symbols-out*"))
        (errfile (make-temp-file "bpfman-symbols-err"))
        (query (bpfman--symbols-query))
        status)
    (unwind-protect
        (progn
          (setq status (bpfman--run-symbols-query query outbuf errfile))
          (unless (and (integerp status) (= status 0))
            (bpfman--report-symbols-error errfile status))
          (with-current-buffer outbuf
            (let ((json-object-type 'alist)
                  (json-array-type 'list)
                  (json-key-type 'symbol))
              (json-read-from-string (buffer-string)))))
      (kill-buffer outbuf)
      (ignore-errors (delete-file errfile)))))

(defun bpfman--symbols-query ()
  "Return the `bpfman-shell --symbols' query for the current buffer."
  (if (and buffer-file-name
           (not (buffer-modified-p)))
      (list :input 'file :args (list "--symbols" buffer-file-name))
    (list :input 'stdin :args '("--symbols" "-"))))

(defun bpfman--run-symbols-query (query outbuf errfile)
  "Run symbols QUERY, writing stdout to OUTBUF and stderr to ERRFILE."
  (condition-case err
      (pcase (plist-get query :input)
        ('file
         (apply #'call-process
                bpfman-shell-program nil (list outbuf errfile) nil
                (plist-get query :args)))
        ('stdin
         (apply #'call-process-region
                (point-min) (point-max)
                bpfman-shell-program nil (list outbuf errfile) nil
                (plist-get query :args))))
    (file-missing
     (user-error "Cannot run %S: %s"
                 bpfman-shell-program
                 (error-message-string err)))))

(defun bpfman--report-symbols-error (errfile status)
  "Signal a symbols query failure with exit STATUS and diagnostics in ERRFILE."
  (let ((msg (with-temp-buffer
               (ignore-errors (insert-file-contents errfile))
               (string-trim (buffer-string)))))
    (user-error "bpfman-shell --symbols failed (exit %s)%s"
                status
                (if (string-empty-p msg)
                    ""
                  (concat ": " (car (split-string msg "\n")))))))

(defun bpfman--resolve-symbol (name symbols file line col)
  "Resolve NAME in SYMBOLS at FILE, LINE, and COL."
  (car (sort (bpfman--candidate-symbols name symbols file line col)
             (lambda (a b)
               (bpfman--symbol-better-p a b line col)))))

(defun bpfman--candidate-symbols (name symbols file line col)
  "Return visible SYMBOLS named NAME at FILE, LINE, and COL."
  (seq-filter
   (lambda (sym)
     (and (string= name (alist-get 'name sym))
          (bpfman--scope-contains-p (alist-get 'scope sym) file line col)))
   symbols))

(defun bpfman--symbol-better-p (a b line col)
  "Return non-nil when symbol A should win over B at LINE and COL."
  (let ((depth-a (bpfman--scope-size (alist-get 'scope a)))
        (depth-b (bpfman--scope-size (alist-get 'scope b))))
    (if (= depth-a depth-b)
        (bpfman--definition-better-p (alist-get 'def a)
                                     (alist-get 'def b)
                                     line col)
      (< depth-a depth-b))))

(defun bpfman--definition-better-p (a b line col)
  "Return non-nil when definition A should win over B at LINE and COL."
  (let ((a-before (bpfman--pos<= a line col))
        (b-before (bpfman--pos<= b line col)))
    (cond
     ((and a-before (not b-before)) t)
     ((and b-before (not a-before)) nil)
     (a-before (bpfman--pos-before-p b a))
     (t (bpfman--pos-before-p a b)))))

(defun bpfman--xref-definition (identifier symbol query-file)
  "Return an xref item for IDENTIFIER resolved to SYMBOL."
  (let* ((def (alist-get 'def symbol))
         (target-file (bpfman--definition-file def query-file)))
    (and target-file
         (xref-make
          identifier
          (xref-make-file-location
           target-file
           (bpfman--json-int (alist-get 'line def))
           (1- (bpfman--json-int (alist-get 'col def))))))))

(defun bpfman--scope-contains-p (scope file line col)
  "Return non-nil when SCOPE contains FILE, LINE, and COL."
  (and scope
       (bpfman--same-file-p (alist-get 'file scope) file)
       (bpfman--pos<= (alist-get 'start scope) line col)
       (bpfman--pos< line col (alist-get 'end scope))))

(defun bpfman--definition-file (def query-file)
  "Return DEF's target file, falling back to QUERY-FILE for stdin."
  (let ((file (alist-get 'file def)))
    (cond
     ((and buffer-file-name
           (or (null file)
               (string= file "")
               (string= file "-")
               (string= file "<stdin>")))
      buffer-file-name)
     ((and file (not (string= file ""))) file)
     (t query-file))))

(defun bpfman--same-file-p (a b)
  "Return non-nil when A and B identify the same symbols coordinate file."
  (cond
   ((and a b) (string= a b))
   ((and (not a) (not b)) t)
   (t nil)))

(defun bpfman--pos<= (pos line col)
  "Return non-nil when POS is before or equal to LINE and COL."
  (let ((pline (bpfman--json-int (alist-get 'line pos)))
        (pcol (bpfman--json-int (alist-get 'col pos))))
    (or (< pline line)
        (and (= pline line) (<= pcol col)))))

(defun bpfman--pos< (line col pos)
  "Return non-nil when LINE and COL are before POS."
  (let ((pline (bpfman--json-int (alist-get 'line pos)))
        (pcol (bpfman--json-int (alist-get 'col pos))))
    (or (< line pline)
        (and (= line pline) (< col pcol)))))

(defun bpfman--pos-before-p (a b)
  "Return non-nil when position A is before position B."
  (let ((aline (bpfman--json-int (alist-get 'line a)))
        (acol (bpfman--json-int (alist-get 'col a)))
        (bline (bpfman--json-int (alist-get 'line b)))
        (bcol (bpfman--json-int (alist-get 'col b))))
    (or (< aline bline)
        (and (= aline bline) (< acol bcol)))))

(defun bpfman--scope-size (scope)
  "Return a coarse sortable size for SCOPE."
  (let* ((start (alist-get 'start scope))
         (end (alist-get 'end scope))
         (start-line (bpfman--json-int (alist-get 'line start)))
         (start-col (bpfman--json-int (alist-get 'col start)))
         (end-line (bpfman--json-int (alist-get 'line end)))
         (end-col (bpfman--json-int (alist-get 'col end))))
    (+ (* (- end-line start-line) 100000)
       (- end-col start-col))))

(defun bpfman--json-int (value)
  "Return VALUE as an integer, accepting JSON numeric shapes."
  (cond
   ((integerp value) value)
   ((numberp value) (truncate value))
   (t 0)))

;; ---- Syntax table ----

(defvar bpfman-mode-syntax-table
  (let ((st (make-syntax-table)))
    ;; # starts a comment to end of line.
    (modify-syntax-entry ?# "<" st)
    (modify-syntax-entry ?\n ">" st)
    ;; Double-quoted strings.
    (modify-syntax-entry ?\" "\"" st)
    ;; Single-quoted strings.
    (modify-syntax-entry ?' "\"" st)
    ;; Underscores are word constituents.
    (modify-syntax-entry ?_ "w" st)
    ;; Brackets, braces, and parens are paired delimiters for
    ;; show-paren-mode.
    (modify-syntax-entry ?\[ "(]" st)
    (modify-syntax-entry ?\] ")[" st)
    (modify-syntax-entry ?{ "(}" st)
    (modify-syntax-entry ?} "){" st)
    (modify-syntax-entry ?\( "()" st)
    (modify-syntax-entry ?\) ")(" st)
    st)
  "Syntax table for `bpfman-mode'.")

;; ---- Mode definition ----

(defvar bpfman-mode-map
  (let ((map (make-sparse-keymap)))
    (define-key map (kbd "C-c C-f") #'bpfman-format-buffer)
    map)
  "Keymap for `bpfman-mode'.")

;;;###autoload
(define-derived-mode bpfman-mode prog-mode "Bpfman"
  "Major mode for editing bpfman-shell scripts.

Provides syntax highlighting, formatting with
\\[bpfman-format-buffer], and xref definitions with \\[xref-find-definitions].
Set `bpfman-shell-program' when bpfman-shell is not on `exec-path'.

\\{bpfman-mode-map}"
  :syntax-table bpfman-mode-syntax-table
  (setq-local comment-start "# ")
  (setq-local comment-end "")
  (setq-local comment-start-skip "#+ *")
  ;; font-lock handles comments and strings via the syntax table
  ;; (syntactic fontification).  Structural highlighting -- commands,
  ;; subcommands, variables, flags -- is layered on top by
  ;; jit-lock-register, which runs our fontifier after font-lock's
  ;; syntactic pass.
  (setq-local font-lock-defaults '(nil))
  (jit-lock-register #'bpfman--fontify-region)
  (setq-local indent-tabs-mode nil)
  (setq-local indent-line-function #'bpfman-indent-line)
  (setq-local electric-indent-chars
              (cons ?} (remove ?} electric-indent-chars)))
  (add-hook 'xref-backend-functions #'bpfman--xref-backend nil t)
  (add-hook 'before-save-hook #'bpfman--maybe-format-before-save nil t))

;;;###autoload
(add-to-list 'auto-mode-alist '("\\.bpfman\\'" . bpfman-mode))

(provide 'bpfman-mode)
;;; bpfman-mode.el ends here
