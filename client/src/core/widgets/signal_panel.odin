package widgets

import "core:fmt"
import "mr:ports"
import "mr:services"
import "mr:ui"

Signal_Panel_Data :: struct {
	store:      ^services.Signal_Store,
	subject_id: u64,
	viewport:   ui.Rect,
	text:       ports.Text_Port,
	max_rows:   int,
}

signal_panel :: proc(buf: ^ui.Command_Buffer, data: Signal_Panel_Data) {
	if buf == nil || data.store == nil || data.subject_id == 0 do return
	max_rows := data.max_rows
	if max_rows <= 0 do max_rows = 3

	recent: [6]services.Signal_Entry
	n := services.signal_store_recent_for_subject(data.store, data.subject_id, recent[:])
	if n <= 0 do return
	if n > max_rows do n = max_rows

	vp := data.viewport
	ui.push(buf, ui.Cmd_Rect_Filled{rect = vp, color = ui.with_alpha(ui.COL_SURFACE_1, 0.86)})
	ui.draw_rect_stroke(buf, vp, ui.with_alpha(ui.COL_BORDER_STRONG, 0.7))
	ui.push_text(buf, {vp.pos.x + 6, vp.pos.y + 11}, "Signals", ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

	y := vp.pos.y + 18
	row_h := f32(19)
	for i in 0 ..< n {
		e := recent[i]
		kind := services.signal_entry_kind_string(&e)
		if len(kind) == 0 do kind = "signal"
		sev := services.signal_entry_severity_string(&e)
		reason := services.signal_entry_reason_string(&e)
		col := signal_severity_color(sev)

		conf := clamp(f32(e.confidence), 0, 1)
		bar_w := (vp.size.x - 10) * conf
		bar_rect := ui.rect_xywh(vp.pos.x + 5, y + row_h - 5, bar_w, 3)
		ui.push(buf, ui.Cmd_Rect_Filled{rect = bar_rect, color = ui.with_alpha(col, 0.9)})

		line_buf: [96]u8
		line := fmt.bprintf(line_buf[:], "%s %.0f%%", kind, conf * 100)
		ui.push_text(buf, {vp.pos.x + 6, y + 9}, line, col, ui.FONT_SIZE_XS, .Mono)
		if len(reason) > 0 && i == 0 {
			reason_buf: [86]u8
			r := reason
			if len(r) > 34 do r = r[:34]
			preview := fmt.bprintf(reason_buf[:], "%s", r)
			ui.push_text(buf, {vp.pos.x + 6, y + row_h + 7}, preview, ui.with_alpha(ui.COL_TEXT_MUTED, 0.85), ui.FONT_SIZE_XS, .Mono)
		}
		y += row_h
		if y > ui.rect_bottom(vp) - 8 do break
	}
}
