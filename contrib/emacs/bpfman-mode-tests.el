;;; bpfman-mode-tests.el --- Tests for bpfman-mode -*- lexical-binding: t; -*-

;;; Commentary:

;; ERT tests for bpfman-mode. Run from this directory with:
;;
;;   emacs -Q --batch -L . -l bpfman-mode.el -l bpfman-mode-tests.el \
;;     -f ert-run-tests-batch-and-exit

;;; Code:

(require 'ert)
(require 'bpfman-mode)

(defconst bpfman-test--directory
  (file-name-directory (or load-file-name buffer-file-name default-directory))
  "Directory containing bpfman-mode-tests.el.")

(defun bpfman-test--shell-program ()
  "Return the built bpfman-shell binary, or skip when it is absent."
  (let ((program (expand-file-name "../bin/bpfman-shell"
                                   bpfman-test--directory)))
    (unless (file-executable-p program)
      (ert-skip "bin/bpfman-shell is not built"))
    program))

(defun bpfman-test--write-file (path contents)
  "Write CONTENTS to PATH."
  (with-temp-file path
    (insert contents)))

(defmacro bpfman-test--with-temp-bpfman-file (contents &rest body)
  "Create a temporary bpfman buffer containing CONTENTS and run BODY."
  (declare (indent 1))
  `(let* ((dir (make-temp-file "bpfman-mode-test-" t))
          (file (expand-file-name "main.bpfman" dir)))
     (unwind-protect
         (progn
           (bpfman-test--write-file file ,contents)
           (find-file-noselect file)
           (with-current-buffer (find-buffer-visiting file)
             (unwind-protect
                 (progn
                   (bpfman-mode)
                   (let ((bpfman-shell-program
                          (bpfman-test--shell-program)))
                     ,@body))
               (set-buffer-modified-p nil)
               (kill-buffer (current-buffer)))))
       (delete-directory dir t))))

(defun bpfman-test--xref-location (identifier)
  "Return the first xref location for IDENTIFIER at point."
  (let ((defs (xref-backend-definitions 'bpfman identifier)))
    (and defs (xref-item-location (car defs)))))

(defun bpfman-test--classified-roles (source)
  "Return (ROLE TEXT) entries classified from SOURCE."
  (with-temp-buffer
    (insert source)
    (goto-char (point-min))
    (mapcar
     (lambda (role)
       (list (nth 0 role)
             (buffer-substring-no-properties (nth 1 role) (nth 2 role))))
     (bpfman--classify-line-tokens
      (bpfman--tokenise-line (point-min) (point-max))))))

(defun bpfman-test--tokens (source)
  "Return (KIND TEXT) tokens from SOURCE."
  (with-temp-buffer
    (insert source)
    (mapcar
     (lambda (tok)
       (list (nth 0 tok)
             (buffer-substring-no-properties (nth 1 tok) (nth 2 tok))))
     (bpfman--tokenise-line (point-min) (point-max)))))

(defun bpfman-test--scan-token-at-start (source)
  "Return (TOKEN-TEXT NEXT-TEXT) for the first scan result in SOURCE."
  (with-temp-buffer
    (insert source)
    (let* ((result (bpfman--scan-token-at (point-min) (point-max)))
           (tok (bpfman--scan-result-token result))
           (next (bpfman--scan-result-next result)))
      (list (and tok
                 (buffer-substring-no-properties (nth 1 tok) (nth 2 tok)))
            (and next
                 (< next (point-max))
                 (buffer-substring-no-properties next (point-max)))))))

(defun bpfman-test--classify-one (state source)
  "Return (NEXT-STATE ROLES) for classifying SOURCE's first token in STATE."
  (with-temp-buffer
    (insert source)
    (let* ((tok (car (bpfman--tokenise-line (point-min) (point-max))))
           (result (bpfman--classify-token-in-state state tok)))
      (list
       (bpfman--classification-state result)
       (mapcar
        (lambda (role)
          (list (nth 0 role)
                (buffer-substring-no-properties (nth 1 role) (nth 2 role))))
        (bpfman--classification-roles result))))))

(defun bpfman-test--face-spans (source fontify)
  "Return (FACE TEXT) spans after applying FONTIFY to SOURCE."
  (with-temp-buffer
    (insert source)
    (funcall fontify (point-min) (point-max))
    (let ((pos (point-min))
          spans)
      (while (< pos (point-max))
        (let* ((face (get-text-property pos 'face))
               (next (or (next-single-property-change pos 'face nil (point-max))
                         (point-max))))
          (when face
            (push (list face
                        (buffer-substring-no-properties pos next))
                  spans))
          (setq pos next)))
      (nreverse spans))))

(defun bpfman-test--interp-segments (source)
  "Return (ROLE TEXT) interpolation segments for SOURCE."
  (with-temp-buffer
    (insert source)
    (mapcar
     (lambda (segment)
       (list (nth 0 segment)
             (buffer-substring-no-properties (nth 1 segment) (nth 2 segment))))
     (bpfman--interp-string-segments (point-min) (point-max)))))

(defun bpfman-test--planned-indents (source)
  "Return planned indentation columns for each line in SOURCE."
  (with-temp-buffer
    (insert source)
    (bpfman-mode)
    (goto-char (point-min))
    (let (indents)
      (while (not (eobp))
        (push (plist-get (bpfman--indentation-plan) :indent) indents)
        (forward-line 1))
      (nreverse indents))))

(ert-deftest bpfman-identifier-at-point-preserves-point ()
  "`bpfman--identifier-at-point' must not move point.

xref calls it immediately before `xref-backend-definitions',
which reads point to locate the reference being resolved. The
tokeniser it uses moves point, so without a `save-excursion' the
definitions lookup runs from the wrong position. This regression
places the reference on the last line, where the drift pushed
point past the half-open end of the binding's scope and the jump
silently resolved nothing -- the case the original smoke test did
not exercise."
  (with-temp-buffer
    (bpfman-mode)
    (insert "def helper(iface prog) { return $prog }\n"
            "let y <- helper eth0 $p\n"
            "print $y\n")
    (goto-char (point-min))
    (forward-line 2)
    (search-forward "$y")
    (backward-char 1)
    (let ((before (point))
          (id (bpfman--identifier-at-point)))
      (should (equal id "y"))
      (should (= (point) before)))))

(ert-deftest bpfman-tokenise-line-uses-thin-token-scanners ()
  "Tokenise representative scanner shapes through the aggregation loop."
  (should (equal (bpfman-test--tokens "$prog.maps[0].name file:${path}")
                 `((,bpfman--tok-varref "$prog.maps[0].name")
                   (,bpfman--tok-adapter-ref "file:${path}"))))
  (should (equal (bpfman-test--tokens "let x <- run |> jq \".\" --dry-run")
                 `((,bpfman--tok-word "let")
                   (,bpfman--tok-word "x")
                   (,bpfman--tok-delim "<-")
                   (,bpfman--tok-word "run")
                   (,bpfman--tok-delim "|>")
                   (,bpfman--tok-word "jq")
                   (,bpfman--tok-string "\".\"")
                   (,bpfman--tok-flag "--dry-run")))))

(ert-deftest bpfman-scan-token-at-reports-next-position ()
  "Scan one token and expose the next unconsumed text."
  (should (equal (bpfman-test--scan-token-at-start "$value rest")
                 '("$value" " rest")))
  (should (equal (bpfman-test--scan-token-at-start "file:$value rest")
                 '("file:$value" " rest"))))

(ert-deftest bpfman-fontify-interp-string-splits-literal-and-interpolation ()
  "Fontify double-quoted interpolation without hiding literal spans."
  (should (equal
           (bpfman-test--interp-segments "\"pre-${name}-post\"")
           '((string "\"pre-")
             (interp-delim "${")
             (interp-body "name")
             (interp-delim "}")
             (string "-post\""))))
  (should (equal
           (bpfman-test--face-spans
            "\"pre-${name}-post\""
            (lambda (beg end) (bpfman--fontify-interp-string beg end)))
           '((font-lock-string-face "\"pre-")
             (font-lock-keyword-face "${")
             (font-lock-variable-name-face "name")
             (font-lock-keyword-face "}")
             (font-lock-string-face "-post\"")))))

(ert-deftest bpfman-indentation-plan-handles-brace-blocks ()
  "Plan shallow block indentation without duplicating the formatter."
  (should (equal (bpfman-test--planned-indents
                  "def helper() {\nlet x = 1\n}\n")
                 '(0 4 0)))
  (should (equal (bpfman-test--planned-indents
                  "def helper() {\nif $ready {\nprint ok\n} else {\nprint no\n}\n}\n")
                 '(0 4 8 4 8 4 0))))

(ert-deftest bpfman-indentation-plan-handles-matches-blocks ()
  "Plan indentation for matches blocks using only brace structure."
  (should (equal (bpfman-test--planned-indents
                  "assert $prog matches exhaustive {\nrecord: matches {\nname: \"demo\"\n}\n}\n")
                 '(0 4 8 4 0))))

(ert-deftest bpfman-indentation-plan-handles-loop-and-poll-blocks ()
  "Plan indentation for foreach and poll through normal brace structure."
  (should (equal (bpfman-test--planned-indents
                  "foreach item in $items {\nprint $item\n}\n")
                 '(0 4 0)))
  (should (equal (bpfman-test--planned-indents
                  "poll every 1s timeout 5s {\nrequire ok bpfman list programs\n}\n")
                 '(0 4 0))))

(ert-deftest bpfman-indentation-plan-ignores-braces-in-strings-and-comments ()
  "Ignore syntax-looking braces inside strings and comments."
  (should (equal (bpfman-test--planned-indents
                  "print \"{\" \n# {\nprint done\n")
                 '(0 0 0))))

(ert-deftest bpfman-indentation-plan-handles-continuations ()
  "Plan one continuation indentation level from the chain start."
  (should (equal (bpfman-test--planned-indents
                  "bpfman program load file \\\nfoo.o \\\n--programs xdp:foo\nprint done\n")
                 '(0 4 4 0)))
  (should (equal (bpfman-test--planned-indents
                  "def helper() {\nbpfman program load file \\\nfoo.o\n}\n")
                 '(0 4 8 0))))

(ert-deftest bpfman-indent-line-applies-indentation-plan ()
  "Interpret the indentation plan by changing only line indentation."
  (with-temp-buffer
    (insert "def helper() {\nlet x = 1\n}\n")
    (bpfman-mode)
    (goto-char (point-min))
    (forward-line 1)
    (bpfman-indent-line)
    (should (equal (buffer-substring-no-properties
                    (line-beginning-position)
                    (line-end-position))
                   "    let x = 1"))
    (should (eq indent-line-function #'bpfman-indent-line))))

(ert-deftest bpfman-classify-line-tokens-let-and-bind-targets ()
  "Classify single-name and destructure binding targets."
  (should (equal (bpfman-test--classified-roles "let value = $input")
                 '((keyword "let")
                   (variable "value")
                   (keyword "=")
                   (variable "$input"))))
  (should (equal (bpfman-test--classified-roles
                  "guard loaded <- bpfman program load file foo.o")
                 '((keyword "guard")
                   (variable "loaded")
                   (keyword "<-")
                   (keyword "bpfman")
                   (builtin "program")
                   (builtin "load")
                   (builtin "file"))))
  (should (equal (bpfman-test--classified-roles "let (a b _) = $items")
                 '((keyword "let")
                   (variable "a")
                   (variable "b")
                   (variable "_")
                   (keyword "=")
                   (variable "$items")))))

(ert-deftest bpfman-classify-token-in-state-describes-transitions ()
  "Classify one token into data before any font-lock effect is applied."
  (should (equal (bpfman-test--classify-one 'start "let")
                 '(let-name ((keyword "let")))))
  (should (equal (bpfman-test--classify-one 'let-name "value")
                 '(let-eq ((variable "value")))))
  (should (equal (bpfman-test--classify-one 'let-eq "=")
                 '(start ((keyword "=")))))
  (should (equal (bpfman-test--classify-one 'subcommand "program")
                 '(args ((builtin "program"))))))

(ert-deftest bpfman-classify-line-tokens-def-foreach-and-assert ()
  "Classify parser-shaped def, foreach, and assertion forms."
  (should (equal (bpfman-test--classified-roles
                  "def helper(iface prog) { return $prog }")
                 '((keyword "def")
                   (variable "helper")
                   (variable "iface")
                   (variable "prog")
                   (keyword "return")
                   (variable "$prog"))))
  (should (equal (bpfman-test--classified-roles
                  "let out <- foreach (id name) in $items {")
                 '((keyword "let")
                   (variable "out")
                   (keyword "<-")
                   (keyword "foreach")
                   (variable "id")
                   (variable "name")
                   (builtin "in")
                   (variable "$items"))))
  (should (equal (bpfman-test--classified-roles
                  "require not ok exec false")
                 '((keyword "require")
                   (builtin "not")
                   (builtin "ok")))))

(ert-deftest bpfman-classify-record-literal ()
  "Classify a record literal: the `record' keyword fontifies as a
builtin, the same as the sibling `matches' brace-block constructor.
The field names inside the block are left plain, matching how
`matches' block keys are treated; only embedded value tokens such
as `$x' carry their own role.  `record' is recognised in both the
binding-RHS (start) position and the argument (after a verb)
position."
  (should (equal (bpfman-test--classified-roles
                  "let r = record { a: 1 b: $x }")
                 '((keyword "let")
                   (variable "r")
                   (keyword "=")
                   (builtin "record")
                   (variable "$x"))))
  (should (equal (bpfman-test--classified-roles
                  "return record { k: $v }")
                 '((keyword "return")
                   (builtin "record")
                   (variable "$v")))))

(ert-deftest bpfman-classify-def-param-type-annotations ()
  "Classify typed def parameters: `name: type'.

The parser accepts optional type annotations on def parameters
(`def f(x: number)', types number/string/bool). The colon glues
to the parameter name in tokenisation, so the name keeps its
variable role and the following type word takes a distinct type
role rather than being mistaken for another parameter name."
  (should (equal (bpfman-test--classified-roles
                  "def f(x: number y: string flag: bool plain) {")
                 '((keyword "def")
                   (variable "f")
                   (variable "x")
                   (type "number")
                   (variable "y")
                   (type "string")
                   (variable "flag")
                   (type "bool")
                   (variable "plain"))))
  ;; A tuple-target list (let/foreach) is not a def parameter
  ;; list, so its tokens never take the type follow-on role.
  (should (equal (bpfman-test--classified-roles
                  "let out <- foreach (id name) in $items {")
                 '((keyword "let")
                   (variable "out")
                   (keyword "<-")
                   (keyword "foreach")
                   (variable "id")
                   (variable "name")
                   (builtin "in")
                   (variable "$items")))))

(ert-deftest bpfman-role-face-maps-type-role-to-type-face ()
  "The def-parameter type role maps to the Emacs type face."
  (should (eq (bpfman--role-face 'type)
              'font-lock-type-face)))

(ert-deftest bpfman-role-face-maps-semantic-roles-to-emacs-faces ()
  "Interpret structural roles as font-lock faces at the boundary."
  (should (eq (bpfman--role-face 'keyword)
              'font-lock-keyword-face))
  (should (eq (bpfman--role-face 'variable)
              'font-lock-variable-name-face))
  (should (eq (bpfman--role-face 'constant)
              'font-lock-constant-face))
  (should (eq (bpfman--role-face 'builtin)
              'font-lock-builtin-face))
  (should-not (bpfman--role-face 'unknown)))

(ert-deftest bpfman-format-query-describes-stdin-formatting ()
  "Describe formatting as stdin input to `bpfman-shell --fmt -'."
  (should (equal (bpfman--format-query)
                 '(:input stdin :args ("--fmt" "-")))))

(ert-deftest bpfman-run-format-query-interprets-stdin-query ()
  "Interpret a format query through `call-process-region'."
  (with-temp-buffer
    (insert "let x = 1\n")
    (let ((region-args nil))
      (cl-letf (((symbol-function 'call-process-region)
                 (lambda (_start _end _program _delete _destination _display
                                  &rest args)
                   (setq region-args args)
                   0)))
        (should (= (bpfman--run-format-query
                    '(:input stdin :args ("--fmt" "-"))
                    (current-buffer)
                    "/tmp/bpfman-mode-test.err")
                   0))
        (should (equal region-args '("--fmt" "-")))))))

(ert-deftest bpfman-resolve-symbol-uses-scope-file-and-def-file ()
  "Imported defs are visible in the querying file but jump to their own file."
  (let* ((symbols
          '(((name . "helper")
             (kind . "def")
             (def . ((file . "lib.bpfman") (line . 1) (col . 5)))
             (scope . ((file . "main.bpfman")
                       (start . ((line . 1) (col . 1)))
                       (end . ((line . 5) (col . 1))))))
            ((name . "helper")
             (kind . "def")
             (def . ((file . "other.bpfman") (line . 1) (col . 5)))
             (scope . ((file . "other.bpfman")
                       (start . ((line . 1) (col . 1)))
                       (end . ((line . 5) (col . 1))))))))
         (match (bpfman--resolve-symbol
                 "helper" symbols "main.bpfman" 3 7)))
    (should match)
    (should (equal (alist-get 'file (alist-get 'def match))
                   "lib.bpfman"))))

(ert-deftest bpfman-symbols-document-uses-file-query-for-clean-file-buffer ()
  "Clean file-backed buffers query their path so imports can resolve."
  (with-temp-buffer
    (setq buffer-file-name "/tmp/main.bpfman")
    (set-buffer-modified-p nil)
    (let ((call-process-args nil)
          (region-called nil))
      (cl-letf (((symbol-function 'call-process)
                 (lambda (_program _infile destination _display &rest args)
                   (setq call-process-args args)
                   (with-current-buffer (car destination)
                     (insert "{\"version\":1,\"file\":\"/tmp/main.bpfman\","
                             "\"symbols\":[],\"errors\":[]}"))
                   0))
                ((symbol-function 'call-process-region)
                 (lambda (&rest _args)
                   (setq region-called t)
                   0)))
        (bpfman--symbols-document)
        (should (equal call-process-args
                       '("--symbols" "/tmp/main.bpfman")))
        (should-not region-called)))))

(ert-deftest bpfman-symbols-query-describes-clean-file-buffer ()
  "Describe an import-aware symbols query for a clean file buffer."
  (with-temp-buffer
    (setq buffer-file-name "/tmp/main.bpfman")
    (set-buffer-modified-p nil)
    (should (equal (bpfman--symbols-query)
                   '(:input file :args ("--symbols" "/tmp/main.bpfman"))))))

(ert-deftest bpfman-symbols-document-uses-stdin-for-modified-buffer ()
  "Modified buffers query stdin so positions match the buffer text."
  (with-temp-buffer
    (setq buffer-file-name "/tmp/main.bpfman")
    (insert "let local = 1\n")
    (set-buffer-modified-p t)
    (let ((call-process-called nil)
          (region-args nil))
      (cl-letf (((symbol-function 'call-process)
                 (lambda (&rest _args)
                   (setq call-process-called t)
                   0))
                ((symbol-function 'call-process-region)
                 (lambda (_start _end _program _delete destination _display
                                  &rest args)
                   (setq region-args args)
                   (with-current-buffer (car destination)
                     (insert "{\"version\":1,\"file\":\"-\","
                             "\"symbols\":[],\"errors\":[]}"))
                   0)))
        (bpfman--symbols-document)
        (should (equal region-args '("--symbols" "-")))
        (should-not call-process-called)))))

(ert-deftest bpfman-symbols-query-describes-modified-buffer-stdin ()
  "Describe a local-only symbols query for modified buffer text."
  (with-temp-buffer
    (setq buffer-file-name "/tmp/main.bpfman")
    (insert "let local = 1\n")
    (set-buffer-modified-p t)
    (should (equal (bpfman--symbols-query)
                   '(:input stdin :args ("--symbols" "-"))))))

(ert-deftest bpfman-xref-jumps-to-imported-def ()
  "A clean file-backed buffer resolves imported defs across files."
  (let* ((dir (make-temp-file "bpfman-mode-test-" t))
         (main (expand-file-name "main.bpfman" dir))
         (lib (expand-file-name "lib.bpfman" dir)))
    (unwind-protect
        (progn
          (bpfman-test--write-file lib
                                   "def helper(x) {\n  return $x\n}\n")
          (bpfman-test--write-file main
                                   "import ./lib.bpfman\nlet v <- helper 1\n")
          (with-current-buffer (find-file-noselect main)
            (unwind-protect
                (let ((bpfman-shell-program
                       (bpfman-test--shell-program)))
                  (bpfman-mode)
                  (goto-char (point-min))
                  (search-forward "helper")
                  (let ((loc (bpfman-test--xref-location "helper")))
                    (should loc)
                    (should (equal (xref-location-group loc) lib))
                    (should (= (xref-location-line loc) 1))
                    (should (= (xref-file-location-column loc) 4))))
              (set-buffer-modified-p nil)
              (kill-buffer (current-buffer)))))
      (delete-directory dir t))))

(ert-deftest bpfman-xref-modified-buffer-does-not-resolve-imported-def ()
  "Modified buffers use stdin mode, so imported defs are unavailable."
  (let* ((dir (make-temp-file "bpfman-mode-test-" t))
         (main (expand-file-name "main.bpfman" dir))
         (lib (expand-file-name "lib.bpfman" dir)))
    (unwind-protect
        (progn
          (bpfman-test--write-file lib
                                   "def helper(x) {\n  return $x\n}\n")
          (bpfman-test--write-file main
                                   "import ./lib.bpfman\nlet v <- helper 1\n")
          (with-current-buffer (find-file-noselect main)
            (unwind-protect
                (let ((bpfman-shell-program
                       (bpfman-test--shell-program)))
                  (bpfman-mode)
                  (goto-char (point-max))
                  (insert "# unsaved\n")
                  (goto-char (point-min))
                  (search-forward "helper")
                  (should-not (bpfman-test--xref-location "helper")))
              (set-buffer-modified-p nil)
              (kill-buffer (current-buffer)))))
      (delete-directory dir t))))

(ert-deftest bpfman-xref-modified-buffer-resolves-local-def ()
  "Modified buffers still resolve definitions in the current buffer."
  (bpfman-test--with-temp-bpfman-file
      "let y = 1\nprint $y\n"
    (goto-char (point-max))
    (insert "# unsaved\n")
    (goto-char (point-min))
    (forward-line 1)
    (search-forward "$y")
    (backward-char 1)
    (let ((loc (bpfman-test--xref-location "y")))
      (should loc)
      (should (equal (xref-location-group loc) buffer-file-name))
      (should (= (xref-location-line loc) 1))
      (should (= (xref-file-location-column loc) 4)))))

(ert-deftest bpfman-xref-broken-import-keeps-local-definitions ()
  "A broken import does not prevent local xref results."
  (bpfman-test--with-temp-bpfman-file
      "import ./missing.bpfman\nlet local = 1\nprint $local\n"
    (goto-char (point-min))
    (forward-line 2)
    (search-forward "$local")
    (backward-char 1)
    (let ((loc (bpfman-test--xref-location "local")))
      (should loc)
      (should (equal (xref-location-group loc) buffer-file-name))
      (should (= (xref-location-line loc) 2))
      (should (= (xref-file-location-column loc) 4)))))

(provide 'bpfman-mode-tests)
;;; bpfman-mode-tests.el ends here
