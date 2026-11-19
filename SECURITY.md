# Security Policy

## Supported Versions

| Version | Supported |
|---|---|
| the latest v2 minor release | yes |
| earlier v2 minor releases | no: move to the latest; patch releases keep the promise in [Compatibility](https://vuka-lang.github.io/project/compatibility) |
| v1.x | no: v1 receives no releases, security fixes included |

v2 is the last major version, so it gets security fixes for as long as Vuka
is maintained; see
[Compatibility](https://vuka-lang.github.io/project/compatibility#support-periods).

## Reporting a Vulnerability

Please report a vulnerability privately, not in a public issue or discussion.

Report it through GitHub's private vulnerability reporting:
[open a report](https://github.com/vuka-lang/vuka/security/advisories/new). Only
the maintainers see it. Use it for a vulnerability in any of the modules —
vuka, ui, web or orm.

Include:

- the module and version (`vuka version` prints the set an app uses);
- what you found and what an attacker gains by it;
- steps or a small program that reproduces it;
- whether it is already public.

## What Happens Next

You get an answer within 7 days saying whether the report is accepted as a
vulnerability. A fix ships as a patch release, outside the weekly
cadence, and its [release note](https://vuka-lang.github.io/releases) says what
was fixed and who found it, unless you ask to stay unnamed.
