# Frontend browser integration

The independent frontend checkout lives in `frontend/` with its own Git history.
It is not a backend dependency or submodule. Do not stage it in this repository.

Google start/link optionally accept `returnTo`: an HTTPS URL ending in `/auth.html`
on APP_ORIGIN or an explicitly allowed frontend origin, without query or fragment.
The target is stored with the single-use flow and checked again before redirecting.
The callback never trusts a callback-supplied target. Existing clients that omit
the parameter retain JSON callback behavior.

For top-level login navigation add `redirect=1` to Google start. Linking remains an
authenticated POST returning Google's URL. Successful browser callbacks set the
Secure, HttpOnly refresh cookie and redirect without access credentials in the URL
or response body. The frontend then refreshes with credentials. Known failures
append a safe auth_error code. Provider cancellation or invalid state/binding
returns an HTTP error without trusting a navigation target.

Migration 00012 adds the target column with an empty default for legacy flows.
It does not reset accounts, sessions, email queues or staging data.

## Verification

Global-app tests use disposable PostgreSQL for allowlisting, state/browser binding,
cookie flags, callback target tampering, flow replay, refresh completion and secure
linking for an existing email. No external Google request is made.

With the separate frontend checkout built, Playwright Chromium installed and Node
22 available, run from this backend root:

```sh
go test -race -tags frontend_e2e ./internal/app -run TestFrontendWithRealAPI -count=1 -timeout=4m -v
```

The optional suite uses temporary loopback HTTPS and its own PostgreSQL cluster.
Registration, OTP, password login, refresh rotation, profile privacy, directory,
sermon publication and logout use the real global app. Inbox and role fixtures
exist only in the build-tagged `_test.go` harness. It never reads DATABASE_URL,
sends external email or modifies staging. Normal Go tests do not require the
frontend checkout or browser installation.
