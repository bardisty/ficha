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

Savings
  What the cache-read tokens would have cost at the full input rate, minus
  what they cost as cache reads. It doesn't subtract the extra paid to write
  the cache, so it's a comparison with uncached list prices, not money saved.

Messages and turns
  A message is one response from the API, and the counts are messages. A
  turn, one prompt from you, usually produces several: each tool call Claude
  makes ends one response and starts another. Agents' messages are counted
  with the session's unless a line says "parent".

Message insights
  In show, insights cover the parent transcript only, without agents, and
  say "scope: parent transcript" when agents ran. breakdown covers every
  message, parent messages first, then each agent's. Peak is the most
  expensive single message, shown when it's more than 1.5x the average.
  Trend compares the average cost of the first 3 messages with the last 3,
  in that order: it needs 6 messages, and a change of 20% or less counts
  as stable.

Context
  How full the model's context window was on the latest parent message:
  its fresh input plus cache writes plus cache reads, the figure Claude
  Code's /context shows, out of the model's window. It's a snapshot, not a
  running total, and agents have context windows of their own.

Agents and workflows
  Agents are sub-sessions Claude started. Their cost is included in the
  session's total, and AGENT SUB-SESSIONS lists them. Agents from a workflow
  run are grouped under the run's name, with the run's subtotal.`,
	}
}
