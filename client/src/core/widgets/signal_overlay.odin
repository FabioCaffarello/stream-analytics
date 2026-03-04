package widgets

import "core:fmt"
import "mr:ports"
import "mr:services"
import "mr:ui"

Signal_Overlay_Data :: struct {
	store:      ^services.Signal_Store,
	subject_id: u64,
	rect:       ui.Rect,
	text:       ports.Text_Port,
}

signal_severity_color :: proc(sev: string) -> ui.Color {
	switch sev {
	case "critical":
		return ui.COL_RED
	case "high":
		return ui.with_alpha(ui.COL_RED, 0.8)
	case "medium":
		return ui.COL_YELLOW_ACCENT
	case "low":
		return ui.COL_ACCENT_CYAN
	}
	return ui.COL_TEXT_MUTED
}

@(private = "file")
signal_severity_short :: proc(sev: string) -> string {
	switch sev {
	case "critical":
		return "CRT"
	case "high":
		return "HI"
	case "medium":
		return "MED"
	case "low":
		return "LO"
	}
	return "SIG"
}

draw_signal_overlay :: proc(buf: ^ui.Command_Buffer, data: Signal_Overlay_Data) {
	if buf == nil || data.store == nil || data.subject_id == 0 do return
	recent: [4]services.Signal_Entry
	n := services.signal_store_recent_for_subject(data.store, data.subject_id, recent[:])
	if n <= 0 do return

	x := data.rect.pos.x
	y := data.rect.pos.y
	max_badges := min(n, 3)
	for i in 0 ..< max_badges {
		entry := recent[i]
		kind := services.signal_entry_kind_string(&entry)
		if len(kind) == 0 do kind = "signal"
		sev := services.signal_entry_severity_string(&entry)
		sev_short := signal_severity_short(sev)

		label_buf: [48]u8
		label := fmt.bprintf(label_buf[:], "%s %s", sev_short, kind)
		bw := ui.status_badge_width(label, data.text.measure, ui.FONT_SIZE_XS)
		bh := f32(14)
		badge_rect := ui.rect_xywh(x, y, bw, bh)
		col := signal_severity_color(sev)
		ui.status_badge(buf, badge_rect, label, col, col, data.text.measure, ui.FONT_SIZE_XS)
		x += bw + 4
		if x > data.rect.pos.x + data.rect.size.x - 40 do break
	}
}
