// Package cobrax collects the cobra and pflag wiring command-line tools keep
// rewriting: a --color[=WHEN] flag, enumerated flags that complete their
// values, file completion switched off where no file is expected, a
// hardened completion command, completion candidates cached on disk, the
// "--flag false" spelling of boolean flags, and version output.
//
// A typical root command:
//
//	root := &cobra.Command{Use: "example", SilenceUsage: true}
//	cobrax.AddColorFlag(root, "when to use color: auto, always or never")
//	output := cobrax.AddEnumFlag(root, root.PersistentFlags(), "output", "o",
//		"text", "output format", "text", "json", "yaml")
//	cobrax.SetVersion(root, buildinfo.Get())
//	root.AddCommand(listCmd, showCmd)
//	cobrax.NoFileCompletions(root)
//	cobrax.HardenCompletion(root)
//	root.SetArgs(cobrax.JoinSpacedBoolValues(root, os.Args[1:]))
package cobrax
