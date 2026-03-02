package ui

// Basic interactive controls rendered via RCL.
// This package stays platform-agnostic: caller passes pointer snapshot and text measure proc.

import "core:fmt"
import "core:math"

Pointer_Input :: struct {
	pos:           Vec2,
	left_down:     bool,
	left_pressed:  bool,
	left_released: bool,
}

Button_Result :: struct {
	hovered: bool,
	pressed: bool,
	clicked: bool,
}

Toggle_Result :: struct {
	value:   bool,
	changed: bool,
	hovered: bool,
}

Segmented_Result :: struct {
	index:   int,
	changed: bool,
}

draw_rect_stroke :: proc(buf: ^Command_Buffer, r: Rect, color: Color, thickness: f32 = 1) {
	if r.size.x <= 0 || r.size.y <= 0 do return
	l := r.pos.x
	t := r.pos.y
	right := rect_right(r)
	bot := rect_bottom(r)
	push(buf, Cmd_Line{from = {l, t}, to = {right, t}, color = color, thickness = thickness})
	push(buf, Cmd_Line{from = {right, t}, to = {right, bot}, color = color, thickness = thickness})
	push(buf, Cmd_Line{from = {right, bot}, to = {l, bot}, color = color, thickness = thickness})
	push(buf, Cmd_Line{from = {l, bot}, to = {l, t}, color = color, thickness = thickness})
}

button :: proc(
	buf: ^Command_Buffer,
	rect: Rect,
	label: string,
	pointer: Pointer_Input,
	measure_proc: proc(size: f32, text: string) -> Vec2,
	font_size: f32 = FONT_SIZE_SM,
	font_id: Font_Id = .Default,
	enabled: bool = true,
) -> Button_Result {
	res: Button_Result
	if rect.size.x <= 0 || rect.size.y <= 0 do return res

	res.hovered = enabled && rect_contains(rect, pointer.pos)
	res.pressed = res.hovered && pointer.left_down
	res.clicked = res.hovered && pointer.left_pressed

	bg := with_alpha(COL_PRIMARY_DIMMED, 0.9)
	fg := with_alpha(COL_WHITE, 0.82)
	if !enabled {
		bg = with_alpha(COL_PRIMARY_DIMMED, 0.45)
		fg = with_alpha(COL_WHITE, 0.40)
	} else if res.pressed {
		bg = adjust_brightness(bg, 0.82)
	} else if res.hovered {
		bg = adjust_brightness(bg, 1.15)
	}

	push(buf, Cmd_Rect_Filled{rect = rect, color = bg})
	border_alpha := f32(0.08)
	if enabled do border_alpha = 0.16
	draw_rect_stroke(buf, rect, with_alpha(COL_WHITE, border_alpha))

	label_size := measure_proc(font_size, label)
	label_pos := align_in_rect(rect, label_size, .Center, .Middle)
	// text is baseline-based; push slightly downward from vertical center.
	label_pos.y += font_size * 0.35
	push_text(buf, label_pos, label, fg, font_size, font_id)

	return res
}

toggle :: proc(
	buf: ^Command_Buffer,
	rect: Rect,
	label: string,
	value: bool,
	pointer: Pointer_Input,
	measure_proc: proc(size: f32, text: string) -> Vec2,
	font_size: f32 = FONT_SIZE_SM,
) -> Toggle_Result {
	btn := button(buf, rect, label, pointer, measure_proc, font_size)
	out := Toggle_Result{
		value   = value,
		changed = false,
		hovered = btn.hovered,
	}
	if btn.clicked {
		out.value = !value
		out.changed = true
	}

	indicator_w := math.min(rect.size.y - 6, 14)
	if indicator_w > 2 {
		indicator := Rect{
			pos  = {rect.pos.x + 4, rect.pos.y + (rect.size.y - indicator_w) * 0.5},
			size = {indicator_w, indicator_w},
		}
		push(buf, Cmd_Rect_Filled{rect = indicator, color = out.value ? COL_GREEN : with_alpha(COL_WHITE, 0.18)})
		draw_rect_stroke(buf, indicator, with_alpha(COL_WHITE, 0.35))
	}

	return out
}

segmented_control :: proc(
	buf: ^Command_Buffer,
	rect: Rect,
	options: []string,
	selected_idx: int,
	pointer: Pointer_Input,
	measure_proc: proc(size: f32, text: string) -> Vec2,
	font_size: f32 = FONT_SIZE_XS,
	font_id: Font_Id = .Mono,
) -> Segmented_Result {
	out := Segmented_Result{index = selected_idx}
	if len(options) == 0 || rect.size.x <= 0 || rect.size.y <= 0 do return out

	seg_gap := f32(2)
	count := len(options)
	total_gap := seg_gap * f32(max(count - 1, 0))
	seg_w := (rect.size.x - total_gap) / f32(count)
	if seg_w <= 0 do return out

	clicked_idx := -1
	if pointer.left_pressed && rect_contains(rect, pointer.pos) {
		for i in 0 ..< count {
			x := rect.pos.x + f32(i) * (seg_w + seg_gap)
			seg := Rect{pos = {x, rect.pos.y}, size = {seg_w, rect.size.y}}
			if rect_contains(seg, pointer.pos) {
				clicked_idx = i
				break
			}
		}
	}

	for i in 0 ..< count {
		x := rect.pos.x + f32(i) * (seg_w + seg_gap)
		seg := Rect{pos = {x, rect.pos.y}, size = {seg_w, rect.size.y}}
		hovered := rect_contains(seg, pointer.pos)
		pressed := hovered && pointer.left_down
		selected := i == selected_idx

		bg := with_alpha(COL_PRIMARY_DIMMED, 0.92)
		fg := with_alpha(COL_WHITE, 0.72)
		border_alpha := f32(0.15)
		if selected {
			bg = with_alpha(COL_BLUE, 0.60)
			fg = with_alpha(COL_WHITE, 0.96)
			border_alpha = 0.30
		}
		if pressed {
			bg = adjust_brightness(bg, 0.70)
			border_alpha += 0.12
		} else if hovered {
			bg = adjust_brightness(bg, 1.20)
			border_alpha += 0.06
		}

		push(buf, Cmd_Rect_Filled{rect = seg, color = bg})
		draw_rect_stroke(buf, seg, with_alpha(COL_WHITE, border_alpha))

		label := options[i]
		label_size := measure_proc(font_size, label)
		label_pos := align_in_rect(seg, label_size, .Center, .Middle)
		label_pos.y += font_size * 0.35
		push_text(buf, label_pos, label, fg, font_size, font_id)

		if i == clicked_idx && i != selected_idx {
			out.index = i
			out.changed = true
		}
	}

	return out
}

// --- Status badge: colored dot + label pill ---

status_badge :: proc(
	buf: ^Command_Buffer,
	rect: Rect,
	label: string,
	dot_color: Color,
	text_color: Color,
	measure_proc: proc(size: f32, text: string) -> Vec2,
	font_size: f32 = FONT_SIZE_XS,
) {
	if rect.size.x <= 0 || rect.size.y <= 0 do return

	// Pill background.
	push(buf, Cmd_Rect_Filled{rect = rect, color = with_alpha(COL_PRIMARY_DIMMED, 0.7)})
	draw_rect_stroke(buf, rect, with_alpha(COL_WHITE, 0.12))

	// Dot.
	dot_sz := f32(6)
	dot_x := rect.pos.x + 6
	dot_y := rect.pos.y + (rect.size.y - dot_sz) * 0.5
	push(buf, Cmd_Rect_Filled{rect = {pos = {dot_x, dot_y}, size = {dot_sz, dot_sz}}, color = dot_color})

	// Label.
	label_size := measure_proc(font_size, label)
	label_x := dot_x + dot_sz + 5
	label_y := rect.pos.y + (rect.size.y - label_size.y) * 0.5 + font_size * 0.35
	push_text(buf, {label_x, label_y}, label, text_color, font_size, .Mono)
}

// Measure the width needed for a status_badge with the given label.
status_badge_width :: proc(
	label: string,
	measure_proc: proc(size: f32, text: string) -> Vec2,
	font_size: f32 = FONT_SIZE_XS,
) -> f32 {
	// 6 pad + 6 dot + 5 gap + text + 8 pad
	return 6 + 6 + 5 + measure_proc(font_size, label).x + 8
}

// --- Icon button: small square with single char ---

icon_button :: proc(
	buf: ^Command_Buffer,
	rect: Rect,
	icon_char: string,
	pointer: Pointer_Input,
	measure_proc: proc(size: f32, text: string) -> Vec2,
	font_size: f32 = FONT_SIZE_XS,
	enabled: bool = true,
) -> Button_Result {
	return button(buf, rect, icon_char, pointer, measure_proc, font_size, .Mono, enabled)
}

// --- Label:Value pair ---

label_value :: proc(
	buf: ^Command_Buffer,
	rect: Rect,
	label: string,
	value: string,
	label_color: Color,
	value_color: Color,
	measure_proc: proc(size: f32, text: string) -> Vec2,
	font_size: f32 = FONT_SIZE_XS,
) {
	if rect.size.x <= 0 || rect.size.y <= 0 do return

	label_size := measure_proc(font_size, label)
	y := rect.pos.y + (rect.size.y - label_size.y) * 0.5 + font_size * 0.35
	push_text(buf, {rect.pos.x, y}, label, label_color, font_size, .Mono)

	value_x := rect.pos.x + label_size.x + 4
	push_text(buf, {value_x, y}, value, value_color, font_size, .Mono)
}

// Measure the total width of a label_value pair.
label_value_width :: proc(
	label: string,
	value: string,
	measure_proc: proc(size: f32, text: string) -> Vec2,
	font_size: f32 = FONT_SIZE_XS,
) -> f32 {
	return measure_proc(font_size, label).x + 4 + measure_proc(font_size, value).x
}

// --- Collapsible section: expand/collapse with chevron ---

Section_State :: struct {
	expanded: bool,
}

Section_Result :: struct {
	content_rect: Rect,
	toggled:      bool,
}

// Draw a collapsible section header with chevron. Returns the content rect (valid only when expanded).
// The header takes `header_h` pixels. Content starts below it.
collapsible_section :: proc(
	buf: ^Command_Buffer,
	rect: Rect,
	title: string,
	state: ^Section_State,
	pointer: Pointer_Input,
	measure_proc: proc(size: f32, text: string) -> Vec2,
	font_size: f32 = FONT_SIZE_XS,
	header_h: f32 = 22,
) -> Section_Result {
	result: Section_Result
	if rect.size.x <= 0 || rect.size.y <= 0 do return result

	// Header row.
	hdr_rect := Rect{pos = rect.pos, size = {rect.size.x, header_h}}
	hovered := rect_contains(hdr_rect, pointer.pos)

	if hovered {
		push(buf, Cmd_Rect_Filled{rect = hdr_rect, color = with_alpha(COL_WHITE, 0.04)})
	}

	// Chevron.
	chevron := state.expanded ? "v" : ">"
	chev_y := hdr_rect.pos.y + header_h * 0.5 + font_size * 0.35
	push_text(buf, {hdr_rect.pos.x + 4, chev_y}, chevron,
		COL_TEXT_MUTED, font_size, .Mono)

	// Title.
	push_text(buf, {hdr_rect.pos.x + 16, chev_y}, title,
		COL_TEXT_MUTED, font_size, .Bold)

	// Divider line under header.
	push(buf, Cmd_Line{
		from      = {rect.pos.x, rect.pos.y + header_h},
		to        = {rect_right(rect), rect.pos.y + header_h},
		color     = COL_DIVIDER,
		thickness = 1,
	})

	// Click to toggle.
	if hovered && pointer.left_pressed {
		state.expanded = !state.expanded
		result.toggled = true
	}

	// Content rect (only meaningful when expanded).
	if state.expanded {
		result.content_rect = Rect{
			pos  = {rect.pos.x, rect.pos.y + header_h + 2},
			size = {rect.size.x, rect.size.y - header_h - 2},
		}
	}

	return result
}

// --- Tooltip: multi-line info box ---

TOOLTIP_MAX_LINES :: 8

Tooltip_Line :: struct {
	label: string,
	value: string,
	color: Color,
}

Tooltip_Data :: struct {
	lines: [TOOLTIP_MAX_LINES]Tooltip_Line,
	count: int,
}

// Draw a tooltip box at the given anchor position. Returns the bounding rect.
// Automatically offsets to stay within clip bounds (viewport).
draw_tooltip :: proc(
	buf: ^Command_Buffer,
	anchor: Vec2,
	data: Tooltip_Data,
	measure_proc: proc(size: f32, text: string) -> Vec2,
	clip_rect: Rect,
	font_size: f32 = FONT_SIZE_XS,
) -> Rect {
	if data.count <= 0 do return {}

	prev_z := buf.current_z_layer
	buf.current_z_layer = Z_TOOLTIP
	defer { buf.current_z_layer = prev_z }

	pad_x := f32(8)
	pad_y := f32(6)
	line_h := measure_proc(font_size, "X").y + 3
	box_h := line_h * f32(data.count) + pad_y * 2

	// Measure max width.
	max_w := f32(0)
	for i in 0 ..< data.count {
		line := data.lines[i]
		w := measure_proc(font_size, line.label).x + 4 + measure_proc(font_size, line.value).x
		if w > max_w do max_w = w
	}
	box_w := max_w + pad_x * 2

	// Position: prefer right and below anchor, but flip if near edges.
	x := anchor.x + 12
	y := anchor.y + 12
	if x + box_w > rect_right(clip_rect) {
		x = anchor.x - box_w - 8
	}
	if y + box_h > rect_bottom(clip_rect) {
		y = anchor.y - box_h - 8
	}
	if x < clip_rect.pos.x do x = clip_rect.pos.x
	if y < clip_rect.pos.y do y = clip_rect.pos.y

	box := Rect{pos = {x, y}, size = {box_w, box_h}}

	// Background + border.
	push(buf, Cmd_Rect_Filled{rect = box, color = with_alpha(COL_SURFACE_2, 0.95)})
	draw_rect_stroke(buf, box, COL_BORDER_STRONG)

	// Lines.
	ly := y + pad_y
	for i in 0 ..< data.count {
		line := data.lines[i]
		text_y := ly + line_h - 3
		push_text(buf, {x + pad_x, text_y}, line.label,
			COL_TEXT_MUTED, font_size, .Mono)
		label_w := measure_proc(font_size, line.label).x + 4
		push_text(buf, {x + pad_x + label_w, text_y}, line.value,
			line.color, font_size, .Mono)
		ly += line_h
	}

	return box
}

// --- Dropdown: trigger button + overlay list ---

DROPDOWN_MAX_OPTIONS :: 16

Dropdown_State :: struct {
	open:        bool,
	selected_idx: int,
	hovered_idx:  int,
}

Dropdown_Result :: struct {
	selected_idx: int,
	changed:      bool,
}

// Draw a dropdown trigger button. When open, renders an overlay list below.
// The overlay_buf parameter should be the SAME command buffer — caller must ensure
// this is called LAST (after all other widgets) for correct z-order.
dropdown :: proc(
	buf: ^Command_Buffer,
	trigger_rect: Rect,
	options: []string,
	state: ^Dropdown_State,
	pointer: Pointer_Input,
	measure_proc: proc(size: f32, text: string) -> Vec2,
	font_size: f32 = FONT_SIZE_XS,
) -> Dropdown_Result {
	result := Dropdown_Result{selected_idx = state.selected_idx}
	if len(options) == 0 || trigger_rect.size.x <= 0 do return result

	// Trigger button: shows selected option + arrow.
	selected_label := "---"
	if state.selected_idx >= 0 && state.selected_idx < len(options) {
		selected_label = options[state.selected_idx]
	}

	trigger_res := button(buf, trigger_rect, selected_label, pointer, measure_proc, font_size, .Mono)
	// Arrow indicator.
	arrow := state.open ? "^" : "v"
	arrow_x := rect_right(trigger_rect) - measure_proc(font_size, arrow).x - 4
	arrow_y := trigger_rect.pos.y + trigger_rect.size.y * 0.5 + font_size * 0.35
	push_text(buf, {arrow_x, arrow_y}, arrow, COL_TEXT_MUTED, font_size, .Mono)

	if trigger_res.clicked {
		state.open = !state.open
	}

	// Overlay list when open (rendered at Z_OVERLAY for correct stacking).
	if state.open {
		prev_z := buf.current_z_layer
		buf.current_z_layer = Z_OVERLAY
		defer { buf.current_z_layer = prev_z }
		item_h := measure_proc(font_size, "X").y + 8
		list_h := item_h * f32(len(options))
		list_rect := Rect{
			pos  = {trigger_rect.pos.x, rect_bottom(trigger_rect) + 2},
			size = {trigger_rect.size.x, list_h},
		}

		// Background.
		push(buf, Cmd_Rect_Filled{rect = list_rect, color = COL_SURFACE_1})
		draw_rect_stroke(buf, list_rect, with_alpha(COL_WHITE, 0.2))

		state.hovered_idx = -1
		for i in 0 ..< len(options) {
			if i >= DROPDOWN_MAX_OPTIONS do break
			item_rect := Rect{
				pos  = {list_rect.pos.x, list_rect.pos.y + f32(i) * item_h},
				size = {list_rect.size.x, item_h},
			}
			hovered := rect_contains(item_rect, pointer.pos)
			if hovered {
				state.hovered_idx = i
				push(buf, Cmd_Rect_Filled{rect = item_rect, color = with_alpha(COL_WHITE, 0.08)})
			}
			if i == state.selected_idx {
				push(buf, Cmd_Rect_Filled{rect = item_rect, color = with_alpha(COL_BLUE, 0.2)})
			}

			text_y := item_rect.pos.y + item_h * 0.5 + font_size * 0.35
			push_text(buf, {item_rect.pos.x + 6, text_y}, options[i],
				hovered ? COL_TEXT_PRIMARY : COL_TEXT_SECONDARY, font_size, .Mono)

			if hovered && pointer.left_pressed {
				if i != state.selected_idx {
					result.selected_idx = i
					result.changed = true
					state.selected_idx = i
				}
				state.open = false
			}
		}

		// Close if clicked outside list and trigger.
		if pointer.left_pressed && !rect_contains(list_rect, pointer.pos) && !rect_contains(trigger_rect, pointer.pos) {
			state.open = false
		}
	}

	return result
}

// --- Context Menu: flat popup list triggered by right-click ---

CONTEXT_MENU_MAX_ITEMS :: 24

Context_Menu_Item :: struct {
	label:    string,
	selected: bool,
	divider:  bool,
}

Context_Menu_State :: struct {
	open:        bool,
	pos:         Vec2,
	hovered_idx: int,
}

Context_Menu_Result :: struct {
	clicked_idx: int, // -1 if nothing clicked
}

context_menu :: proc(
	buf: ^Command_Buffer,
	state: ^Context_Menu_State,
	items: []Context_Menu_Item,
	pointer: Pointer_Input,
	measure_proc: proc(size: f32, text: string) -> Vec2,
	clip_rect: Rect,
	font_size: f32 = FONT_SIZE_XS,
) -> Context_Menu_Result {
	result := Context_Menu_Result{clicked_idx = -1}
	if !state.open || len(items) == 0 do return result

	prev_z := buf.current_z_layer
	buf.current_z_layer = Z_OVERLAY
	defer { buf.current_z_layer = prev_z }

	pad_x := f32(6)
	pad_y := f32(4)
	item_h := measure_proc(font_size, "X").y + 6

	// Count dividers for extra height.
	div_count := 0
	for item in items { if item.divider do div_count += 1 }

	// Measure max label width.
	max_w := f32(0)
	for item in items {
		w := measure_proc(font_size, item.label).x
		if w > max_w do max_w = w
	}
	menu_w := max_w + pad_x * 2 + 12
	menu_h := item_h * f32(len(items)) + pad_y * 2 + f32(div_count) * 4

	// Position: prefer below-right, flip if near edges.
	x := state.pos.x
	y := state.pos.y
	if x + menu_w > rect_right(clip_rect) do x = state.pos.x - menu_w
	if y + menu_h > rect_bottom(clip_rect) do y = state.pos.y - menu_h
	if x < clip_rect.pos.x do x = clip_rect.pos.x
	if y < clip_rect.pos.y do y = clip_rect.pos.y

	menu_rect := Rect{pos = {x, y}, size = {menu_w, menu_h}}

	// Background + border.
	push(buf, Cmd_Rect_Filled{rect = menu_rect, color = with_alpha(COL_SURFACE_2, 0.97)})
	draw_rect_stroke(buf, menu_rect, COL_BORDER_STRONG)

	// Items.
	state.hovered_idx = -1
	iy := y + pad_y
	for i in 0 ..< len(items) {
		if i >= CONTEXT_MENU_MAX_ITEMS do break
		item := items[i]

		// Divider line before this item.
		if item.divider {
			push(buf, Cmd_Line{
				from = {x + 4, iy + 1}, to = {x + menu_w - 4, iy + 1},
				color = COL_DIVIDER, thickness = 1,
			})
			iy += 4
		}

		item_rect := Rect{pos = {x, iy}, size = {menu_w, item_h}}
		hovered := rect_contains(item_rect, pointer.pos)

		if hovered {
			state.hovered_idx = i
			push(buf, Cmd_Rect_Filled{rect = item_rect, color = with_alpha(COL_WHITE, 0.08)})
		}
		if item.selected {
			push(buf, Cmd_Rect_Filled{
				rect = {pos = {x + 2, iy + 2}, size = {3, item_h - 4}},
				color = COL_BLUE,
			})
		}

		text_y := iy + item_h * 0.5 + font_size * 0.35
		fg := hovered ? COL_TEXT_PRIMARY : (item.selected ? COL_TEXT_PRIMARY : COL_TEXT_SECONDARY)
		push_text(buf, {x + pad_x + 6, text_y}, item.label, fg, font_size, .Mono)

		if hovered && pointer.left_pressed {
			result.clicked_idx = i
			state.open = false
		}

		iy += item_h
	}

	// Close if clicked outside.
	if pointer.left_pressed && !rect_contains(menu_rect, pointer.pos) {
		state.open = false
	}

	return result
}

// --- Number stepper ---

Stepper_Result :: struct {
	value:   int,
	changed: bool,
}

// Compact [-] value [+] stepper control.
// Layout: [label: NN  - +] within the given rect.
stepper :: proc(
	buf: ^Command_Buffer,
	rect: Rect,
	label: string,
	value: int,
	lo, hi: int,
	pointer: Pointer_Input,
	measure_proc: proc(size: f32, text: string) -> Vec2,
	font_size: f32 = FONT_SIZE_XS,
) -> Stepper_Result {
	result := Stepper_Result{value = value}
	if rect.size.x <= 0 || rect.size.y <= 0 do return result

	btn_w := f32(16)
	gap := f32(2)
	h := rect.size.y

	// [+] button on the right.
	plus_rect := Rect{pos = {rect_right(rect) - btn_w, rect.pos.y}, size = {btn_w, h}}
	plus_hov := rect_contains(plus_rect, pointer.pos)
	plus_bg := plus_hov ? with_alpha(COL_WHITE, 0.12) : with_alpha(COL_WHITE, 0.05)
	push(buf, Cmd_Rect_Filled{rect = plus_rect, color = plus_bg})
	push_text(buf, {plus_rect.pos.x + 4, plus_rect.pos.y + h * 0.5 + font_size * 0.35}, "+",
		with_alpha(COL_WHITE, 0.7), font_size, .Mono)
	if plus_hov && pointer.left_pressed && value < hi {
		result.value = value + 1
		result.changed = true
	}

	// [-] button to the left of [+].
	minus_rect := Rect{pos = {plus_rect.pos.x - btn_w - gap, rect.pos.y}, size = {btn_w, h}}
	minus_hov := rect_contains(minus_rect, pointer.pos)
	minus_bg := minus_hov ? with_alpha(COL_WHITE, 0.12) : with_alpha(COL_WHITE, 0.05)
	push(buf, Cmd_Rect_Filled{rect = minus_rect, color = minus_bg})
	push_text(buf, {minus_rect.pos.x + 4, minus_rect.pos.y + h * 0.5 + font_size * 0.35}, "-",
		with_alpha(COL_WHITE, 0.7), font_size, .Mono)
	if minus_hov && pointer.left_pressed && value > lo {
		result.value = value - 1
		result.changed = true
	}

	// Value number to the left of [-].
	val_buf: [8]u8
	val_str := fmt.bprintf(val_buf[:], "%d", value)
	val_w := measure_proc(font_size, val_str).x
	val_x := minus_rect.pos.x - val_w - 4
	push_text(buf, {val_x, rect.pos.y + h * 0.5 + font_size * 0.35}, val_str,
		with_alpha(COL_WHITE, 0.8), font_size, .Mono)

	// Label on the left.
	push_text(buf, {rect.pos.x + 2, rect.pos.y + h * 0.5 + font_size * 0.35}, label,
		COL_TEXT_SECONDARY, font_size, .Mono)

	return result
}

// Float stepper with 0.1 step (displayed as "0.5", "2.0", etc.)
Stepper_Float_Result :: struct {
	value:   f64,
	changed: bool,
}

stepper_float :: proc(
	buf: ^Command_Buffer,
	rect: Rect,
	label: string,
	value: f64,
	lo, hi, step: f64,
	pointer: Pointer_Input,
	measure_proc: proc(size: f32, text: string) -> Vec2,
	font_size: f32 = FONT_SIZE_XS,
) -> Stepper_Float_Result {
	result := Stepper_Float_Result{value = value}
	if rect.size.x <= 0 || rect.size.y <= 0 do return result

	btn_w := f32(16)
	gap := f32(2)
	h := rect.size.y

	plus_rect := Rect{pos = {rect_right(rect) - btn_w, rect.pos.y}, size = {btn_w, h}}
	plus_hov := rect_contains(plus_rect, pointer.pos)
	plus_bg := plus_hov ? with_alpha(COL_WHITE, 0.12) : with_alpha(COL_WHITE, 0.05)
	push(buf, Cmd_Rect_Filled{rect = plus_rect, color = plus_bg})
	push_text(buf, {plus_rect.pos.x + 4, plus_rect.pos.y + h * 0.5 + font_size * 0.35}, "+",
		with_alpha(COL_WHITE, 0.7), font_size, .Mono)
	if plus_hov && pointer.left_pressed && value + step <= hi + 0.001 {
		result.value = value + step
		result.changed = true
	}

	minus_rect := Rect{pos = {plus_rect.pos.x - btn_w - gap, rect.pos.y}, size = {btn_w, h}}
	minus_hov := rect_contains(minus_rect, pointer.pos)
	minus_bg := minus_hov ? with_alpha(COL_WHITE, 0.12) : with_alpha(COL_WHITE, 0.05)
	push(buf, Cmd_Rect_Filled{rect = minus_rect, color = minus_bg})
	push_text(buf, {minus_rect.pos.x + 4, minus_rect.pos.y + h * 0.5 + font_size * 0.35}, "-",
		with_alpha(COL_WHITE, 0.7), font_size, .Mono)
	if minus_hov && pointer.left_pressed && value - step >= lo - 0.001 {
		result.value = value - step
		result.changed = true
	}

	val_buf: [8]u8
	val_str := fmt.bprintf(val_buf[:], "%.1f", value)
	val_w := measure_proc(font_size, val_str).x
	val_x := minus_rect.pos.x - val_w - 4
	push_text(buf, {val_x, rect.pos.y + h * 0.5 + font_size * 0.35}, val_str,
		with_alpha(COL_WHITE, 0.8), font_size, .Mono)

	push_text(buf, {rect.pos.x + 2, rect.pos.y + h * 0.5 + font_size * 0.35}, label,
		COL_TEXT_SECONDARY, font_size, .Mono)

	return result
}
