# Security Policy

## Supported Versions

| Version | Supported |
|---|---|
| the latest v2 minor release | yes |
| earlier v2 minor releases | no: move to the latest; patch releases keep the promise in [Compatibility](https://vuka-lang.github.io/project/compatibility) |
| v1.x | no: v1 receives no releases, security fixes included |

How long a major version gets security fixes after the next one ships is on
the [Compatibility](https://vuka-lang.github.io/project/compatibility#support-periods)
page.

## Reporting a Vulnerability

Please report a vulnerability privately, not in a public issue or discussion.

TODO(owner): a security contact (email, or enable GitHub private vulnerability reporting and link it)

Include:

- the module and version (`vuka version` prints the set an app uses);
- what you found and what an attacker gains by it;
- steps or a small program that reproduces it;
- whether it is already public.

## What Happens Next

You get an answer saying whether the report is accepted as a vulnerability,
TODO(owner): response time. A fix ships as a patch release, outside the weekly
cadence, and its [release note](https://vuka-lang.github.io/releases) says what
was fixed and who found it, unless you ask to stay unnamed.
