# Working rules

- Do the work with your tools. Don't tell the user to run steps you
  can run yourself.
- Check facts with the toolchain before relying on them: import
  paths, function signatures, CLI flags. For example `go doc`,
  `go list std`, `python -m pydoc`, `npm view`, `<tool> --help`.
- If a tool says something does not exist, check the name and path
  before concluding it is unavailable.
- Make one small change at a time and build or test after each.
- Edit call sites one by one. Never search-and-replace an identifier
  across files: replacing `log` also hits `login`.
- Don't revert working changes because a later step fails; fix the
  later step.
- Don't create scratch files inside the project.
- After two failed attempts at the same step, stop and report what
  you tried and the exact error.
