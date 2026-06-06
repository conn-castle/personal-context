# Personal Context — Vision & Purpose

> _Written in Nick's voice. This is the product intent — the "why" behind Personal
> Context — that architecture, requirements, and storage decisions should serve.
> Requirements such as portability and durability follow from this vision and are
> addressed in their own documents._

## Origin

Personal Context started as a simple way to keep track of work — a replacement for
slides that an agent can read, create, and interact with while an AI researcher (or
any researcher) does their work. That original use case still matters to me, and I
still want it.

## The larger ambition

Since then the scope has grown, because what I fundamentally want is for Personal
Context to store **every bit of knowledge a user has ever created**.

I'm a scientist and an engineer. I do both traditional software development and deep
scientific research, and I want one place that holds the knowledge from all of it.

I'll be honest about the risk: trying to make Personal Context the end-all, be-all
home for every piece of data that researchers and agents generate may be causing more
problems than it solves. It's a lofty goal — but it's one I still want to try to
achieve.

## The three needs

Everything Personal Context is for comes down to three high-level needs.

### 1. An agent-authored lab notebook

An agent-created presentation that a human can review and look at — enriched data and
artifacts, all produced by an agent. Humans tend to be visual by nature, so this gives
me a very easy way to digest what an agent has created. It makes for a smooth
experience for research and experimental-type work. Think of it like a lab notebook,
but augmented with agents.

### 2. Keep every artifact agents generate

As I've taken on more projects, I've realized agents create a ton of artifacts as they
develop — just look at the temporary folder in this directory, or any of the others I
work in. I have a fundamental belief that all generated data has intrinsic value, even
if any single piece isn't worth much. There's something there. In a world where
storage is practically free, I don't want to lose anything that gets created.

Part of this is a bet on the future: my hypothesis is that within about five years, AI
will let us interact with every piece of knowledge we've ever created with zero
friction. But even in the meantime, there are huge benefits to giving agents access to
the historical body of knowledge from everything I've ever wrestled with. You should
never have to wrestle with the same bug twice, blind.

### 3. Keep and use every chat

Every single chat has incredible value too. I want agents to meet my needs based on my
chat history, and I picture a world where my agents can look back at what we've talked
about before when we get stuck. I've only been doing this for about a year, and there's
already around 10 GB of potentially useful data sitting around.

## Why one tool, not three

All three needs are really about the same thing: **data in general, and historical
knowledge.** That shared core is why I'm trying to bring them together into a single
tool rather than build separate ones.

I can be talked out of this — it may be cleaner to build two or three separate tools
instead. But I worry that splitting them would water down the vision I have for what
Personal Context can be.

## Castle Vault

Castle Vault is my local, personal copy of Personal Context.
