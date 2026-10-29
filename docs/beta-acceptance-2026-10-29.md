# Beta acceptance — 29 October 2026

## Released versions checked

- Backend `main`: `aaea500f0e3dceef0ba4ed02248d8790440e95b6`.
- Frontend `main`: `eeaafc9aed5d6c76a48ce9199c9575c88f0fe754`.
- API: https://city4christ-staging-api.onrender.com.
- Frontend: https://city4christ-pwa.vercel.app.

## Live checks completed

Twenty automated, read-only checks passed. No staging account, password, role,
sermon, note or playback position was changed; no email was requested.

- API readiness, published sermon listing and series listing returned HTTP 200.
- Anonymous profile, staff sermon detail, private notes and progress returned 401.
- An untrusted origin was rejected with 403.
- The real frontend origin's authenticated note PUT preflight returned 204 with
  the exact allowed origin and credential support.
- Desktop Chromium and mobile-sized touch Chromium loaded signup, redirected
  anonymous profile access to login, searched/opened an existing published sermon,
  hid private notes from anonymous visitors and reported no unhandled page errors.

This is Chromium mobile emulation, not Safari/device acceptance. A visible signup
form does not establish successful email delivery or account creation. Anonymous
denial does not establish that an authenticated ordinary member is denied staff
actions. Those checks remain separate.

## Disposable PostgreSQL privacy checks

The following targeted race-detector run passed for both packages on current main:

```sh
go test -race ./internal/sermons ./internal/app -run 'TestMemberProgressAndNotesArePrivateAndBounded|TestSermonProgressAndPrivateNotesThroughGlobalApp' -count=1 -timeout=4m
```

It verifies separate account ownership of notes and progress, input bounds,
unpublished-sermon exclusion, deletion behavior and authenticated global-app
routes. These are local PostgreSQL results, not signed-in staging/device acceptance.
The full backend test suite was not rerun during this acceptance pass.

## Private device acceptance still required

Device feedback received during this pass: profile reload kept the user signed in;
after logout, reopening the profile sent the user to login. Both were confirmed by
the user. Google linking then exposed a frontend integration problem: the React app
sent users through older test-console pages. A local frontend fix keeps Google flows
in React; real Google acceptance must be repeated after that fix is released.

1. Register a new approved beta test address, receive its OTP and verify it.
   Keep the password and OTP private.
2. Sign in, reload the profile page, navigate away and return. Confirm session
   restoration works with the device's real cross-site cookie policy.
3. Save a synthetic private sermon note and playback position, reload, and verify
   persistence. Check from a second ordinary test account that it cannot read the
   first account's note.
4. Confirm an ordinary member cannot create/publish sermons or delegate roles.
5. Test Google login and explicit linking. Matching an existing email must request
   explicit linking; a Google-only account must not acquire a local password.
6. Sign out and reload a protected page. It must request sign-in again.

Record outcomes and request IDs for failures. Do not record passwords, OTPs, Google
codes, access/refresh tokens or recovery codes in reports, screenshots or chat.
These checks do not authorize production rollout or staging resets.

## Local repository reconciliation

Both independent working repositories were fast-forwarded to their released
`origin/main`. Older source changes were preserved in local-only branches named
`archive/pre-beta-20261029`:

- Backend snapshot: `4f2fcf5`.
- Frontend snapshot: `c54fe77`.

The source feature files already exist in main through merged PRs. Archive branches
contain older implementations and migration numbering; do not deploy them or merge
them wholesale. Any unique change must be reviewed and ported onto current main.
The original PRD and repository-boundary instructions were left untouched.
