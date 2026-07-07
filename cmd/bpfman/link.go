package main

// LinkCmd groups link management subcommands.
type LinkCmd struct {
	// Attach attaches an already-loaded program to a hook, creating a new
	// link (the per-type attachment is selected by the nested subcommand).
	Attach AttachCmd `cmd:"" help:"Attach a loaded program to a hook."`

	// Detach detaches one or more links by link ID, removing the kernel
	// attachment but leaving the program loaded.
	Detach DetachCmd `cmd:"" help:"Detach a link."`

	// Get prints the details of a single link identified by its link ID.
	Get GetLinkCmd `cmd:"" help:"Get details of a link by link ID."`

	// List lists managed links, optionally filtered; it is the default
	// subcommand when "link" is given no further verb.
	List ListLinksCmd `cmd:"" default:"withargs" help:"List managed links."`

	// Delete deletes a link and performs the associated cascading cleanup
	// (for example tearing down an empty dispatcher).
	Delete LinkDeleteCmd `cmd:"" hidden:"" help:"Delete a link with cascading cleanup."`
}
