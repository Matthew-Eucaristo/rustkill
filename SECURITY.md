# Security policy

rustkill moves and deletes files and runs rustup and cargo commands, so any of these is a security issue:

- it removes, moves or changes something outside the rules in the README's [safety model](README.md#safety-model), or follows a symlink out of an item;
- it removes something git tracks or does not ignore, or an item a running process uses;
- it runs a command that is not the owning tool's documented cleanup, or runs one through a shell with untrusted input;
- it writes to another tool's state or configuration.

## Reporting

Please report privately through GitHub: **Security → Report a vulnerability** on this repository. Do not open a public issue.

Include the rustkill version, OS, the rustup and cargo versions involved, and steps to reproduce using placeholder paths. You can expect an acknowledgement within a week.

## Supported versions

Only the latest release receives fixes.
