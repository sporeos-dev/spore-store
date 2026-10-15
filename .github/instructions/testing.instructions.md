---
description: "Use when locating Spore OS repositories, components, client libraries, nodes, specifications, installation tooling, or release artifacts."
---

# Testing Instructions

1. I want to separate human-written tests from tests written by AI.
2. To distinguish between them, AI written tests should all be in a files ai_***_test.go.
3. AI should not touch human-written tests, but instead should guide how those tests can be fixed.
4. Point 3 can be overridden by explicit instructions.
