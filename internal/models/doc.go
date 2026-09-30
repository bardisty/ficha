// Package models holds the data types the other packages pass between them:
// raw transcript lines as parsed, token usage and cost breakdowns, and the
// session, agent, project and global analyses built from them.
//
// The MarshalJSON methods here define the json of show, summary and global,
// so a field or tag change is a change to the machine-output contract in
// docs/machine-output.md. list's json records and all csv live in
// internal/formatter. Apart from that encoding the package has almost no
// behavior; parsing, pricing and analysis live in their own packages.
package models
