// Package cliformat renders bpfman CLI command results for display.
//
// Every command that prints output -- program load and list, link
// attach, get and list, dispatcher inspection -- routes its result
// through a Render function here, so the text and JSON encodings live
// in one place rather than being scattered across the command
// handlers. The Render functions (RenderProgram, RenderProgramList,
// RenderLinkList, RenderDispatcherSnapshot, and the rest) take the
// domain value they render together with an OutputFormat and write
// either a human-readable table or JSON to the supplied writer. The JSON
// shape is the domain/result type's own encoding; the view types below
// exist only where the text table needs presentation-only fields the
// domain object does not carry.
//
// Two shared renderers do the layout. Detail (get) views -- program,
// link, and dispatcher get -- build a tree of label/value rows and
// render it through renderRows, which derives indentation from depth.
// List views -- program, link, and dispatcher list, and the show
// sub-tables -- build a header and string cells and render them through
// renderTable, the single space-aligned table writer. The remaining view
// types (LinkGetView, LinkListView) adapt the manager's domain objects
// into the shape the text renderers expect.
package cliformat
