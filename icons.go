package main

// Inline SVG icons for the account table's action buttons.
//
// Icons rather than labels because three labelled buttons do not fit the last
// column of a table at phone width — the row overflowed and only the first button
// stayed reachable. Each icon is about a third of the width of its label.
//
// Inline markup rather than an icon font or sprite: the panel is embedded and ships
// no assets, and an SVG in the markup inherits currentColor so it follows the theme
// without a second definition for dark mode.
//
// Every icon here is decorative in the sense that the button always carries a
// title and an aria-label; these strings exist to be drawn, not to be read.
const (
	// iconCheck marks a check-in action.
	iconCheck = `<svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true" focusable="false">` +
		`<path d="M3 8.5l3.2 3.2L13 5" fill="none" stroke="currentColor" stroke-width="1.8" ` +
		`stroke-linecap="round" stroke-linejoin="round"/></svg>`

	// iconCoin marks a credit/quota refresh.
	iconCoin = `<svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true" focusable="false">` +
		`<circle cx="8" cy="8" r="5.6" fill="none" stroke="currentColor" stroke-width="1.6"/>` +
		`<path d="M9.6 6.2c-.4-.6-1-.9-1.7-.9-1 0-1.7.6-1.7 1.4 0 2 3.6.9 3.6 2.8 0 .8-.8 1.4-1.9 1.4-.9 0-1.6-.4-1.9-1" ` +
		`fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg>`

	// iconPowerOn is shown for an account that is currently serving, so the button
	// offers to stop it.
	iconPowerOn = `<svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true" focusable="false">` +
		`<path d="M8 2.4v5.2" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/>` +
		`<path d="M4.6 4.8a4.6 4.6 0 1 0 6.8 0" fill="none" stroke="currentColor" stroke-width="1.8" ` +
		`stroke-linecap="round"/></svg>`

	// iconPowerOff is shown for a parked account, so the button offers to start it.
	iconPowerOff = `<svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true" focusable="false">` +
		`<path d="M8 1.6v5.2" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/>` +
		`<path d="M3.6 3.9a6 6 0 1 0 8.8 0" fill="none" stroke="currentColor" stroke-width="1.8" ` +
		`stroke-linecap="round"/></svg>`
)
