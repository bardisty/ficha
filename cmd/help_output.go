package cmd

import "github.com/spf13/cobra"

// newOutputHelpTopic is `ficha help output`, a glossary for the terms the
// reports use. It has no Run, so cobra lists it under "Additional help
// topics" rather than as a command.
func newOutputHelpTopic() *cobra.Command {
	return &cobra.Command{
		Use:   "output",
		Short: "Reading the output: estimates, cache, savings, context",
		Long: `Reading the output

Estimates
  Every dollar figure is an API-equivalent estimate: tokens priced at
  Anthropic's list prices. On a Claude subscription you aren't billed per
  token, so read a total as what the same work would cost through the API,
  not as a bill.

Input, Output, Cache write, Cache read
  Input is fresh prompt tokens and Output is what Claude wrote. Claude Code
  caches the conversation so each request can reuse it. Writing to the cache
  costs more than plain input: 1.25x the input rate for the 5-minute cache
  (5m TTL) and 2x for the 1-hour cache (1h TTL). Reading from it costs a
  fraction: 10% of the input rate on most models, less on some newer ones.
  breakdown's columns call them IN, OUT, C_WR and C_RD.

Savings
  What the cache-read tokens would have cost at the full input rate, minus
  what they cost as cache reads. It doesn't subtract the extra paid to write
  the cache, so it's a comparison with uncached list prices, not money saved.

Messages and turns
  A message is one response from the API, and the counts are messages. A
  turn, one prompt from you, usually produces several: each tool call Claude
  makes ends one response and starts another. Agents' messages are counted
  with the session's, except in a count marked "main". Lines Claude Code
  writes itself, such as an API error, count too, the same in every report.
  They cost $0, breakdown names their model "synthetic", and COST BY MODEL
  leaves them out.

Forks
  A forked session's transcript starts with copies of the original's
  replies. list, summary and global count each reply once, under the
  session it first appeared in, so a project's sessions add up to its
  total. show, watch, breakdown and statusline price one transcript on
  its own, copies included, so a fork costs more there than in list. In
  list, a fork that added nothing reads "(copy)", before its title when
  it has one.

Message insights
  In show and watch, insights cover the main conversation only, and say
  "main conversation only, agents excluded" when agents ran. breakdown
  covers every message, agents included. Peak is the most expensive single
  message, shown when it's more than 1.5x the average; in breakdown, p
  jumps to it and then to the next most expensive, and s sorts the table
  by cost. Trend compares the average cost of the last 20 messages (the
  later half of a shorter session) with the session's average: it needs 6
  messages, and a difference of 20% or less reads as flat.

Context
  How full the model's context window was at the main conversation's
  latest message: its fresh input plus cache writes plus cache reads, the
  figure Claude Code's /context shows, out of the model's window. It's a
  snapshot, not a running total. A request larger than a 200K model's
  window reads against the 1M long-context window it must have run on. For
  a model ficha doesn't know, or a request larger than any window ficha
  knows for its model, ficha shows the tokens alone and says why. Agents
  have context windows of their own: AGENT SUB-SESSIONS reads each one's
  the same way, from its latest request.

~compaction
  The context bar's tick marks where Claude Code auto-compacts the
  conversation by default: about 967K tokens on models with a native 1M
  window, and the end of the window on the rest, per
  code.claude.com/docs/en/model-config. It's approximate, and your Claude
  Code settings can move it. The reading turns orange 85% of the way to
  the mark and red at it, and the note under the bar counts the tokens to
  it or past it. From the orange band on, statusline says the same in
  words and colors its percentage. A running agent in its own orange band
  or past its mark, and nearer it than the main conversation is to its
  own, is named under the bar and in watch's footer, as "(agent 98%)".
  In breakdown, a divider above the first reply after a compaction says
  whether it was auto or manual (/compact) and how large the context was
  when it compacted: "compacted (auto, 180.0K)". That reply writes the
  summary to the cache, so it costs more, and the rows after it less.

Agents and workflows
  Agents are sub-sessions Claude started. Their cost is included in the
  session's total, and AGENT SUB-SESSIONS lists them, each with its task
  beside its ID when there's room. Agents from a workflow run are grouped
  under the run's name, with the run's subtotal. In watch, a dot marks an
  agent that wrote in the last 5 minutes, unless its workflow run has
  ended, and the section's rule says how many are running, as in "4 of 10
  running". In breakdown, a workflow agent's AGENT cell ends in its run's
  initials, such as rc for review-changes, the footer names those on
  screen when it has room, and a wide terminal adds the task after the
  cell.

Spend rate
  watch's footer shows it as $46.82/h (10m): what the last 10 minutes
  cost, agents included, scaled to an hour. It reads -/h until the session
  is 10 minutes old, since a shorter stretch scaled up would read one
  expensive message as a steady burn. After a failed reload it keeps the
  last rate for 10 minutes, then reads -/h.

Cost trend
  watch's COST TREND chart plots what each clock minute cost, agents
  included: "$ per minute, last 68m". The newest column is the minute in
  progress. The chart moves on each minute even while nothing is written,
  so a pause shows as empty columns. It spans 68 minutes at 80 columns
  and up to 110 on a wider terminal, fewer on a narrower one or in a
  younger session. It draws a line, or bars with --ascii. The scale runs
  from $0 to the costliest minute on screen, which max gives. Like the
  spend rate, it skips messages without a timestamp. A short terminal
  leaves it out.`,
	}
}
