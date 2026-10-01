# Screenshot Timestamp Rule

Applies universally — any screenshot submitted as evidence, on any control
type (DESIGN or OE), must show the laptop's OS clock (taskbar/menu-bar)
at the moment of capture.

An app-displayed "Generated at" or "Report time" timestamp inside the
screenshotted application does **not** satisfy this rule — it only proves
when the underlying report was generated, not when the screenshot itself
was taken. Look specifically for the operating system's own clock: the
Windows taskbar clock (bottom-right) or the macOS menu-bar clock (top-right).

A screenshot missing a visible OS clock is a gap — report it in
`gaps_found` with `severity: "MEDIUM"` unless a more specific rule (such as
the Population Completeness Proof rule) requires it at a higher severity for
that particular screenshot's role.
