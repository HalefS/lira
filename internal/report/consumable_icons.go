package report

import "html/template"

// consumableIconSVG holds the inner markup of every consumable icon, so the
// printed report draws the same glyph the application does.
//
// GENERATED from CONSUMABLE_ICONS in cmd/api/ui/index.html -- the two maps
// below are generated, the function at the end is maintained by hand.
// TestConsumableIconsMatchCatalog fails if the Go catalog (data.ConsumableIcons)
// and these maps drift apart.
//
// Every value is trusted static markup, never user input, which is why it is
// template.HTML rather than a plain string that would get escaped.
var consumableIconSVG = map[string]template.HTML{
	"battery-aa":   template.HTML(`<rect x="7.5" y="5" width="9" height="17" rx="2.2" fill="currentColor" fill-opacity=".16"/> <rect x="10.2" y="2" width="3.6" height="3" rx=".8"/> <text x="12" y="13.7" transform="rotate(-90 12 13.7)" font-size="6.2" fill="currentColor" stroke="none" font-family="'DM Sans',system-ui,sans-serif" font-weight="700" text-anchor="middle" dominant-baseline="central">AA</text>`),
	"battery-aaa":  template.HTML(`<rect x="8.4" y="5" width="7.2" height="17" rx="2" fill="currentColor" fill-opacity=".16"/> <rect x="10.4" y="2" width="3.2" height="3" rx=".8"/> <text x="12" y="13.7" transform="rotate(-90 12 13.7)" font-size="5" fill="currentColor" stroke="none" font-family="'DM Sans',system-ui,sans-serif" font-weight="700" text-anchor="middle" dominant-baseline="central">AAA</text>`),
	"battery-9v":   template.HTML(`<rect x="6" y="7.5" width="12" height="14.5" rx="2.2" fill="currentColor" fill-opacity=".16"/> <circle cx="9.6" cy="5" r="1.7"/><circle cx="14.4" cy="5" r="1.7"/> <text x="12" y="15.3" font-size="6.4" fill="currentColor" stroke="none" font-family="'DM Sans',system-ui,sans-serif" font-weight="700" text-anchor="middle" dominant-baseline="central">9V</text>`),
	"battery-coin": template.HTML(`<circle cx="12" cy="12" r="8.6" fill="currentColor" fill-opacity=".16"/> <circle cx="12" cy="12" r="5.2" stroke-width="1.3"/> <path d="M12 9.6v4.8M9.6 12h4.8" stroke-width="1.5"/>`),
	"phone":        template.HTML(`<path d="M3.5 8.2v-.8C3.5 6.1 4.6 5 5.9 5h12.2c1.3 0 2.4 1.1 2.4 2.4v.8c0 .8-.7 1.5-1.5 1.5h-1.9c-.8 0-1.4-.6-1.4-1.4V7.3H9.5v1c0 .8-.6 1.4-1.4 1.4H5c-.8 0-1.5-.7-1.5-1.5z" fill="currentColor" fill-opacity=".16"/> <path d="M7.2 12.2h9.6l3.4 7.8H3.8z" fill="currentColor" fill-opacity=".16"/> <path d="M9.4 15.2h.01M12 15.2h.01M14.6 15.2h.01M9.4 17.5h.01M12 17.5h.01M14.6 17.5h.01" stroke-width="1.9"/>`),
	"mobile":       template.HTML(`<rect x="6.8" y="2.5" width="10.4" height="19" rx="2.6" fill="currentColor" fill-opacity=".16"/> <path d="M10.4 5.4h3.2"/><path d="M10.6 18.8h2.8"/>`),
	"tablet":       template.HTML(`<rect x="4.5" y="2.5" width="15" height="19" rx="2.6" fill="currentColor" fill-opacity=".16"/> <circle cx="12" cy="18.6" r=".9" fill="currentColor" stroke="none"/>`),
	"remote":       template.HTML(`<rect x="8" y="2" width="8" height="20" rx="3.2" fill="currentColor" fill-opacity=".16"/> <circle cx="12" cy="6.2" r="1.5" stroke-width="1.5"/> <circle cx="10" cy="10.6" r=".95" fill="currentColor" stroke="none"/><circle cx="14" cy="10.6" r=".95" fill="currentColor" stroke="none"/> <circle cx="10" cy="13.6" r=".95" fill="currentColor" stroke="none"/><circle cx="14" cy="13.6" r=".95" fill="currentColor" stroke="none"/> <path d="M10.2 17.6h3.6"/>`),
	"tv":           template.HTML(`<rect x="2.5" y="4.5" width="19" height="13" rx="2.3" fill="currentColor" fill-opacity=".16"/> <path d="M10.6 8.6v4.8l4-2.4z" stroke-width="1.5"/> <path d="M7 21l1.2-3.5M17 21l-1.2-3.5"/>`),
	"monitor":      template.HTML(`<rect x="2.5" y="3.5" width="19" height="13" rx="2.3" fill="currentColor" fill-opacity=".16"/> <path d="M6.5 8h6M6.5 11.2h3.6" stroke-width="1.5"/> <path d="M12 16.5V20M8 20.5h8"/>`),
	"hdmi":         template.HTML(`<path d="M4.5 3.5h15v5.2l-3 3.8h-9l-3-3.8z" fill="currentColor" fill-opacity=".16"/> <path d="M8.3 6.2v2.4M10.8 6.2v2.4M13.2 6.2v2.4M15.7 6.2v2.4" stroke-width="1.5"/> <path d="M9 12.5v3.5h6v-3.5"/><path d="M12 16v5.5"/>`),
	"ethernet":     template.HTML(`<rect x="6" y="3.5" width="12" height="11.5" rx="1.9" fill="currentColor" fill-opacity=".16"/> <path d="M9 6.2v3.6M11.2 6.2v3.6M13.4 6.2v3.6M15.6 6.2v3.6" stroke-width="1.4"/> <path d="M9.4 15v3.6h5.2V15"/><path d="M12 18.6v3"/>`),
	"charger":      template.HTML(`<rect x="6" y="8" width="12" height="9.4" rx="2.6" fill="currentColor" fill-opacity=".16"/> <path d="M9.6 8V4.4M14.4 8V4.4"/> <path d="M12 17.4v4"/>`),
	"power-strip":  template.HTML(`<rect x="2.5" y="7.5" width="19" height="9" rx="2.4" fill="currentColor" fill-opacity=".16"/> <path d="M6.3 10.6v2.8M8.3 10.6v2.8M11 10.6v2.8M13 10.6v2.8M15.7 10.6v2.8M17.7 10.6v2.8" stroke-width="1.5"/> <path d="M12 16.5v4"/>`),
	"bulb":         template.HTML(`<path d="M12 2.5a6.5 6.5 0 0 0-3.9 11.7c.6.5.9 1.2.9 1.9v.4h6v-.4c0-.7.3-1.4.9-1.9A6.5 6.5 0 0 0 12 2.5z" fill="currentColor" fill-opacity=".16"/> <path d="M9.5 19.2h5M10.6 21.6h2.8"/> <path d="M10.3 9.6 12 12l1.7-2.4M12 12v4.4" stroke-width="1.4"/>`),
	"key-card":     template.HTML(`<rect x="2.5" y="5.5" width="19" height="13" rx="2.4" fill="currentColor" fill-opacity=".16"/> <rect x="2.5" y="8.6" width="19" height="2.8" fill="currentColor" fill-opacity=".5" stroke="none"/> <path d="M6 15.2h5.5"/> <path d="M16.3 13.4a2.4 2.4 0 0 1 0 3.2M18.4 12.2a4.2 4.2 0 0 1 0 5.6" stroke-width="1.5"/>`),
	"lock":         template.HTML(`<rect x="5" y="10.5" width="14" height="10.5" rx="2.6" fill="currentColor" fill-opacity=".16"/> <path d="M8 10.5V8a4 4 0 0 1 8 0v2.5"/> <circle cx="12" cy="15" r="1.4" fill="currentColor" stroke="none"/><path d="M12 15.6v2"/>`),
	"router":       template.HTML(`<rect x="3" y="14" width="18" height="6.5" rx="2.2" fill="currentColor" fill-opacity=".16"/> <path d="M9.6 9.6a3.4 3.4 0 0 1 4.8 0M7.2 7.2a6.8 6.8 0 0 1 9.6 0" stroke-width="1.6"/> <circle cx="12" cy="12" r=".9" fill="currentColor" stroke="none"/> <circle cx="6.8" cy="17.2" r=".85" fill="currentColor" stroke="none"/><circle cx="9.6" cy="17.2" r=".85" fill="currentColor" stroke="none"/> <path d="M14.6 17.2h3.8" stroke-width="1.5"/>`),
	"usb":          template.HTML(`<rect x="8" y="2.5" width="8" height="6" rx=".9"/> <circle cx="10.4" cy="5.5" r=".75" fill="currentColor" stroke="none"/><circle cx="13.6" cy="5.5" r=".75" fill="currentColor" stroke="none"/> <rect x="6.5" y="8.5" width="11" height="13" rx="2.4" fill="currentColor" fill-opacity=".16"/> <circle cx="12" cy="17.6" r="1.3"/>`),
	"mouse":        template.HTML(`<rect x="6.5" y="2.5" width="11" height="19" rx="5.5" fill="currentColor" fill-opacity=".16"/> <path d="M6.5 10.4h11M12 2.5v4"/> <rect x="11" y="5.4" width="2" height="3.4" rx="1" fill="currentColor" stroke="none"/>`),
	"keyboard":     template.HTML(`<rect x="2" y="6" width="20" height="12" rx="2.4" fill="currentColor" fill-opacity=".16"/> <path d="M6 9.8h.01M9 9.8h.01M12 9.8h.01M15 9.8h.01M18 9.8h.01M6 12.6h.01M9 12.6h.01M12 12.6h.01M15 12.6h.01M18 12.6h.01" stroke-width="2.2"/> <path d="M7.5 15.4h9" stroke-width="1.6"/>`),
	"headset":      template.HTML(`<path d="M4.5 14v-2a7.5 7.5 0 0 1 15 0v2"/> <rect x="3" y="13" width="4.2" height="6.6" rx="1.7" fill="currentColor" fill-opacity=".16"/> <rect x="16.8" y="13" width="4.2" height="6.6" rx="1.7" fill="currentColor" fill-opacity=".16"/> <path d="M19 19.6v.3c0 1.5-1.2 2.6-2.7 2.6H14"/> <circle cx="12.6" cy="22.5" r=".9" fill="currentColor" stroke="none"/>`),
	"printer":      template.HTML(`<path d="M7 9V3.5h10V9"/> <rect x="3" y="9" width="18" height="8" rx="2.2" fill="currentColor" fill-opacity=".16"/> <rect x="7" y="14" width="10" height="7" rx=".9" fill="currentColor" fill-opacity=".16"/> <path d="M9.6 17h4.8M9.6 19h2.6" stroke-width="1.4"/> <circle cx="17.6" cy="11.8" r=".8" fill="currentColor" stroke="none"/>`),
	"box":          template.HTML(`<path d="M3.5 7.5 12 3l8.5 4.5v9L12 21l-8.5-4.5z" fill="currentColor" fill-opacity=".16"/> <path d="M3.5 7.5 12 12l8.5-4.5M12 12v9"/>`),
}

// consumableIconTone mirrors CONSUMABLE_ICON_TONE in the UI: the colour family
// each icon is drawn in.
var consumableIconTone = map[string]string{
	"battery-aa":   "amber",
	"battery-aaa":  "amber",
	"battery-9v":   "amber",
	"battery-coin": "amber",
	"phone":        "blue",
	"mobile":       "blue",
	"tablet":       "blue",
	"remote":       "teal",
	"tv":           "teal",
	"monitor":      "teal",
	"hdmi":         "green",
	"ethernet":     "green",
	"charger":      "red",
	"power-strip":  "red",
	"bulb":         "amber",
	"key-card":     "blue",
	"lock":         "blue",
	"router":       "green",
	"usb":          "green",
	"mouse":        "green",
	"keyboard":     "green",
	"headset":      "blue",
	"printer":      "teal",
	"box":          "gray",
}

// iconSVGWrapper matches the svg element the UI wraps CONSUMABLE_ICONS in
// (see ConsumableIcon in cmd/api/ui/index.html), so a printed icon is the same
// drawing at the same weight. The maps hold only the inner shapes, which lets
// the wrapper live in one place instead of at every call site.
const iconSVGWrapper = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" ` +
	`stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">`

// consumableIconFor returns the complete <svg> element and tone for an icon
// key, falling back to the generic box for an unknown key -- the same fallback
// the UI applies.
func consumableIconFor(key string) (template.HTML, string) {
	svg, ok := consumableIconSVG[key]
	if !ok {
		return consumableIconFor("box")
	}
	tone, ok := consumableIconTone[key]
	if !ok {
		tone = consumableIconTone["box"]
	}
	return template.HTML(iconSVGWrapper + string(svg) + `</svg>`), tone
}
