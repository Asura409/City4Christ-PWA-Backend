# Demo sermon records

The preview dataset contains seven records prefixed `[DEMO]`. Five are fictional
sermon copy created only to exercise list, detail, search, card layout, and
image rendering. Two are clearly labeled external BibleProject video previews;
they are not City4Christ sermons. Do not use the demo records as church
announcements or real ministry history.

Run a dry run with the staging API and frontend origins set:

```sh
export CITY4CHRIST_API_URL=https://city4christ-staging-api.onrender.com
export CITY4CHRIST_APP_ORIGIN=https://city4christ-pwa.vercel.app
node scripts/seed-demo-sermons.mjs
```

To add records, use a short-lived access token from a verified content editor.
Read it without echoing or saving it, then run the explicit apply operation:

```sh
read -rsp 'Editor access token: ' CITY4CHRIST_ACCESS_TOKEN; echo
export CITY4CHRIST_ACCESS_TOKEN
node scripts/seed-demo-sermons.mjs --apply
unset CITY4CHRIST_ACCESS_TOKEN
```

The script uses the same create and publish endpoints as the editor, so each
write is permission-checked and audited. It skips exact titles already visible
in the public catalog and never deletes or replaces records. If a run stops
between draft creation and publication, check the editor list before retrying;
the current API does not expose a draft-list route. The script has a safety
allowlist for the City4Christ staging host and loopback development servers.

The demo photos link directly to Unsplash's image CDN, so the browser needs
network access to `images.unsplash.com`. Their source pages identify the
photographers and describe the images:

- [Amonwat Dumkrut — people praying in church](https://unsplash.com/photos/people-standing-and-praying-in-a-church-ns2S6utqxfg)
- [Kaylee Stoll — a church service](https://unsplash.com/photos/people-gathered-inside-a-church-during-a-service-vFHm1J-ZhIQ)
- [Shane Hoving — Bible before stained glass](https://unsplash.com/photos/an-open-book-sitting-in-front-of-a-stained-glass-window-DZ9uK83TA7c)
- [Amonwat Dumkrut — worship singer](https://unsplash.com/photos/woman-singing-in-church-worship-service-3E0KqEVDKzg)
- [Manish Mishra — people praying together](https://unsplash.com/photos/people-pray-together-hands-clasped-V4EdBMaojog)

The video previews point to BibleProject's [Psalms](https://www.youtube.com/watch?v=dpny22k_7uk) and [Book of Psalms Summary](https://www.youtube.com/watch?v=j9phNEaPrv8). The database stores canonical YouTube watch URLs; the React screen builds the privacy-enhanced embed URL at render time.
