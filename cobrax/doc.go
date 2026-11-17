// Package cobrax collects the cobra and pflag wiring command-line tools keep
// rewriting: a --color[=WHEN] flag and other flags that take auto, always
// or never ([AddModeFlag]), enumerated flags that complete their values,
// older spellings of flags kept as hidden switches ([AddFlagAlias]), file
// completion switched off where no file is expected, a hardened completion
// command that can install its scripts for the current user or write them
// to a path of the tool's choice, completion candidates cached on disk,
// filtered by prefix ([CompletePrefix]) and kept free of control characters
// ([SafeCompletions]), the "--flag false" spelling of boolean flags, and
// version output.
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
//	cobrax.AddCompletionInstall(cobrax.HardenCompletion(root))
//	root.SetArgs(cobrax.JoinSpacedBoolValues(root, os.Args[1:]))
package cobrax
