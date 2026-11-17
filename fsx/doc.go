// Package fsx holds file system helpers the standard library leaves out.
//
// [WriteFile] replaces a file atomically, the way a configuration file, a
// generated script or a cache should be written: a reader sees the old
// contents or the new ones, never part of either, also when the program is
// interrupted halfway. A file that holds a credential is written with
// secret.WriteFile instead, which is the same with the mode fixed to 0600.
package fsx
