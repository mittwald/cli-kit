// Package secret finds the passwords and tokens a command-line tool needs,
// and keeps them between runs without writing them anywhere in plain sight.
//
// A [Store] keeps one secret per account name. [Keyring] uses the operating
// system's keyring (macOS Keychain, Windows Credential Manager, the Secret
// Service on Linux desktops). [FileStore] keeps each secret in a file only
// its owner can read, for machines without a keyring, such as servers and
// containers; [Fallback] combines the two, and [Migrating] also moves what
// the files hold into the keyring once there is one. Behind either, a
// secret the keyring does not take is kept in the file, which is not
// encrypted. [Unavailable] stands in for a store that could not be set up.
// [Memory] is for tests; a test binary never reaches the operating system
// keyring (see [Keyring]).
//
// A [Resolver] decides where a secret comes from on a given run. It tries,
// in this order:
//
//  1. a file the user named (a --password-file flag), or "-" for stdin,
//  2. an environment variable,
//  3. a piped stdin,
//  4. the [Store],
//  5. an interactive prompt with hidden input: [TTYPrompt], or a text field
//     of the tool's own through [InputPrompt].
//
// Explicit sources come first, so a secret passed for one run always wins
// over the one remembered from an earlier run. Once the secret proved valid,
// [Resolver.Save] remembers it; when a service rejects it,
// [Resolver.Forget] drops it so the next attempt asks again. A Store that
// cannot be read does not keep the Resolver from asking.
//
// The third source needs care. Any stdin that is not a terminal counts as
// piped, and it is read before the Store is asked. A tool that is run in a
// shell loop reading stdin, in a Git hook, or over ssh without a terminal
// therefore takes a line of someone else's input for the secret, or waits
// for one, although the Store holds the secret. A command that reads its
// own input sets [Resolver.StdinBusy]; so does a tool that wants stdin
// only when the user asks for it with the file "-". See [Resolver].
//
// [WriteFile] writes a file only its owner can read, such as a token cache,
// replacing an existing one atomically.
//
// Nothing in this package prints, logs or embeds a secret value in an error.
// [Redact] removes known secrets from text that is about to be logged, as
// they are and in their URL-escaped and JSON-escaped forms; it does not
// find a secret inside a larger encoded value, such as the Base64 string
// of HTTP Basic authentication.
package secret
