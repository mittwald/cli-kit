// Package clipboard copies text to the system clipboard.
//
// There is no portable system call for this, so [Copy] runs a small chain of
// helper commands for the platform, in order, and the first one found in PATH
// that exits cleanly wins. That keeps the module free of cgo and of X11
// headers, and works the same way on macOS, Windows, WSL, Wayland, X11 and
// Termux:
//
//   - macOS: pbcopy.
//   - Windows: clip, which is given UTF-16LE with a byte-order mark, since it
//     reads 8-bit input in the console's code page and garbles anything that
//     is not ASCII.
//   - Linux and the BSDs: xclip and xsel, then wl-copy; wl-copy first when
//     WAYLAND_DISPLAY is set, because under XWayland xclip appears to work
//     while it writes to a clipboard the Wayland applications do not read.
//     Then termux-clipboard-set for Termux on Android, and clip.exe, which is
//     reachable from WSL, in UTF-16LE as on Windows.
//   - Android: termux-clipboard-set, from the Termux:API package, then the
//     X11 and Wayland helpers.
//
// The whole chain gets 10 seconds: a helper that hangs, such as xclip with an
// X server that does not answer, fails the copy instead of hanging the
// command that asked for it. When no helper works, the error names every
// failure and what to install.
package clipboard
