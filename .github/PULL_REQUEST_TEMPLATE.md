<!--
    Thank you for your contribution to Bpfman! 🎉

    For Work In Progress Pull Requests, please use the Draft PR feature.

-->

# Pre-check

Before submitting a Pull Request, please ensure you've done the following:

- 📖 Read the Bpfman Contributing Guide: https://github.com/bpfman/bpfman/blob/main/CONTRIBUTING.md
- 📖 Read the Bpfman Code of Conduct: https://github.com/bpfman/bpfman/blob/main/CODE_OF_CONDUCT.md
- ✅ Add tests according to our Test Policy: https://github.com/bpfman/bpfman/blob/main/CONTRIBUTING.md#test-policy
- 📝 Use descriptive commit messages: https://cbea.ms/git-commit/
- 📗 Update any related documentation.


# Summary
<!---
      Summarize the changes you're making here.
      Detailed information belongs in the Git Commit messages.
      Feel free to flag anything you thing needs a reviewer's attention.
-->

# Related Issues
<!--
For example:

- Closes: #1234
- Relates To: #1234
-->

# Added/updated tests?

_We strongly encourage you to add a test for your changes._

- [ ] Yes
- [ ] No, and this is why: _please replace this line with details on why tests
      have not been included_
- [ ] I need help with writing tests

# Checklist

- [ ] 📎 All clippy lints have been fixed:
```sh
cd bpfman/
cargo +nightly clippy --all -- --deny warnings
```
- [ ] 🦀 Rust code has been formatted and linted:
```sh
cargo +nightly fmt --all -- --check
```
- [ ] 📝 Yaml files have been formatted (see [Install Yaml Formatter](https://bpfman.io/main/getting-started/building-bpfman/#install-yaml-formatter)):
```sh
prettier -l "*.yaml"
```
- [ ] 🐚 Bash scripts have been linted using `shellcheck`:
```sh
cargo xtask lint
```
- [ ] ✅ Unit tests are passing locally (see [Unit Testing](https://bpfman.io/main/developer-guide/testing/#unit-testing)):
```sh
cargo xtask unit-test
```
- [ ] ✅ Integration tests are passing locally (see [Basic Integration Tests](https://bpfman.io/main/developer-guide/testing/#basic-integration-tests)):
```sh
cargo xtask integration-test
```

# (Optional) What emojis best describe this PR or how it makes you feel?
