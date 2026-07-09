// Package tui hosts gskill's interactive surfaces, rendered over the app
// service layer and refusing to launch without a TTY: the dashboard (a
// bubbles table stacked over a glamour SKILL.md preview viewport), the
// guided onboarding wizard (a step state machine whose agents and version
// steps are huh forms, while the source and skill-picker steps stay custom
// where the keyboard contract demands it), and the bounded, filterable
// multi-select skill picker. One Theme feeds every surface — and the CLI's
// styled output — so the looks cannot drift apart.
package tui
