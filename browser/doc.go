// Package browser opens a web page in the user's browser, such as the
// sign-in page of an OAuth flow.
//
// [Open] runs the platform's launcher, without cgo and without a shell:
// open on macOS, rundll32 url.dll,FileProtocolHandler on Windows, and
// xdg-open everywhere else. It takes http and https URLs only, so a URL
// from elsewhere cannot make the launcher open a local file or start
// another program through a scheme of its own.
//
// Opening a page can fail without anyone noticing: there may be no desktop,
// no browser, or a browser that shows the page on another screen. Print the
// URL as well, so the user can open it by hand.
package browser
