# Security policy

## Reporting a vulnerability

Please **do not open a public issue** for security problems.

Report them privately through GitHub: open the repository's **Security** tab and choose **Report a vulnerability** (private vulnerability reporting). Include what you found, how to reproduce it and the version or commit you tested.

You will get an answer as soon as possible. Once a fix is available, the issue is disclosed in the release notes, with credit if you want it.

## Supported versions

Only the latest commit on `main` is supported. Update by pulling and rebuilding:

```bash
git pull && docker compose up -d --build
```

## Scope

feedbacklooped was written entirely by an AI model (see the README) and has not been independently audited. Reports about authentication, session handling, access to submissions or attachments, upload handling, XSS/CSRF and anything that could expose the identity of anonymous submitters are especially welcome.
