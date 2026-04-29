# Initial Plan

Define ebnf grammars for ipv4 and ipv6 and their various common/standard formatting textual representations. Do the same for subnets/CIDR. Use github.com/accretional/gluon. Validate each on many different examples. Run fuzzing on them too.

I've defined a little service for requesting IP info from a grpc server, you can use that for validation with ana ctual local /proc/net (maybe different for darwin) impl

Implement an initial version of github.com/accretional/proto-fixedlength using IP addresses/cidr as your guiding use case. We may need to change the fixed length descriptors/other formats. The goal here is to link this to the grammars and allow us to convert proto-encoded IP from this repo into raw 128bit IPv6 in a way that generalizes

Make sure to carefully study gluon's codebase to understand the tools available to implement this. Use v2/ wherever possible; eamine git commit history to check freshness. it's mid migration

You can use AST-AST transformations to handle the conversion to fixedlength messages using GrammarDescriptorProto, then another AST-AST transformation back to a single node bytes format I think.

Take notes throughout your work to hlep you EXTENSIVELY in docs/impl-notes.md. Track your progress FREQUENTLY in docs/progress-log.md, maybe even after every few tool calls/actions.

# Setup/Invariants

Make sure you create a setup.sh, build.sh, test.sh, and LET_IT_RIP.sh that contain all project setup scripts/commands used - NEVER build/test/run the code in this repo outside of these scripts, NEVER commit or push without running these either. Make them idempotent so that each build.sh can run setup.sh and skip things already set up, each test.sh can run build.sh, each LET_IT_RIP runs test.sh

Have LET_IT_RIP.sh run queries over the actual local grpc service to get local ips

use go 1.26.
