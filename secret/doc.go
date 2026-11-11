// Package secret finds the passwords and tokens a command-line tool needs,
// and keeps them between runs without writing them anywhere in plain sight.
//
// A [Store] keeps one secret per account name. [Keyring] uses the operating
// system's keyring (macOS Keychain, Windows Credential Manager, the Secret
// Service on Linux desktops). [FileStore] keeps each secret in a file only
// its owner can read, for machines without a keyring, such as servers and
// containers; [Fallback] combines the two. [Memory] is for tests.
//
// A [Resolver] decides where a secret comes from on a given run. It tries,
// in this order:
//
//  1. a file the user named (a --password-file flag), or "-" for stdin,
//  2. an environment variable,
//  3. a piped stdin,
//  4. the [Store],
//  5. an interactive prompt with hidden input.
//
// Explicit sources come first, so a secret passed for one run always wins
// over the one remembered from an earlier run. Once the secret proved valid,
// [Resolver.Save] remembers it; when a service rejects it,
// [Resolver.Forget] drops it so the next attempt asks again.
//
// [WriteFile] writes a file only its owner can read, such as a token cache,
// replacing an existing one atomically.
//
// Nothing in this package prints, logs or embeds a secret value in an error.
// [Redact] removes known secrets from text that is about to be logged.
package secret
