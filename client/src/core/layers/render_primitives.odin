package layers

import "mr:ui"

// Fixed-size primitive buffer used by all layers (zero heap in hot path).

LAYER_OUTPUT_CAP :: 2048
TEXT_BADGE_CAP   :: 96

Render_Primitive_Kind :: enum u8 {
	Line,
	Bar,
	Text_Badge,
}

Render_Line :: struct {
	from:      ui.Vec2,
	to:        ui.Vec2,
	color:     ui.Color,
	thickness: f32,
}

Render_Bar :: struct {
	rect:  ui.Rect,
	color: ui.Color,
}

Render_Text_Badge :: struct {
	pos:      ui.Vec2,
	text:     [TEXT_BADGE_CAP]u8,
	text_len: u8,
	color:    ui.Color,
	size:     f32,
}

Render_Primitive_Data :: struct #raw_union {
	line: Render_Line,
	bar:  Render_Bar,
	text: Render_Text_Badge,
}

Render_Primitive :: struct {
	kind:  Render_Primitive_Kind,
	z:     u8,
	order: u32,
	data:  Render_Primitive_Data,
}

Layer_Outputs :: struct {
	items:      [LAYER_OUTPUT_CAP]Render_Primitive,
	count:      int,
	overflowed: u64,
	next_order: u32,
}

layer_outputs_reset :: proc(out: ^Layer_Outputs) {
	if out == nil do return
	out.count = 0
	out.next_order = 0
}

@(private = "file")
layer_outputs_reserve :: proc(out: ^Layer_Outputs, kind: Render_Primitive_Kind, z: u8) -> (^Render_Primitive, bool) {
	if out == nil do return nil, false
	if out.count >= LAYER_OUTPUT_CAP {
		out.overflowed += 1
		return nil, false
	}
	idx := out.count
	out.count += 1
	item := &out.items[idx]
	item.kind = kind
	item.z = z
	item.order = out.next_order
	out.next_order += 1
	return item, true
}

layer_outputs_push_line :: proc(out: ^Layer_Outputs, z: u8, line: Render_Line) {
	item, ok := layer_outputs_reserve(out, .Line, z)
	if !ok do return
	item.data = Render_Primitive_Data{line = line}
}

layer_outputs_push_bar :: proc(out: ^Layer_Outputs, z: u8, bar: Render_Bar) {
	item, ok := layer_outputs_reserve(out, .Bar, z)
	if !ok do return
	item.data = Render_Primitive_Data{bar = bar}
}

layer_outputs_push_text_badge :: proc(out: ^Layer_Outputs, z: u8, badge: Render_Text_Badge) {
	item, ok := layer_outputs_reserve(out, .Text_Badge, z)
	if !ok do return
	item.data = Render_Primitive_Data{text = badge}
}

text_badge_make :: proc(pos: ui.Vec2, text: string, color: ui.Color, size: f32) -> Render_Text_Badge {
	badge := Render_Text_Badge{
		pos   = pos,
		color = color,
		size  = size,
	}
	n := min(len(text), TEXT_BADGE_CAP)
	for i in 0 ..< n {
		badge.text[i] = text[i]
	}
	badge.text_len = u8(n)
	return badge
}

text_badge_string :: proc(badge: ^Render_Text_Badge) -> string {
	if badge == nil || badge.text_len == 0 do return ""
	n := min(int(badge.text_len), len(badge.text))
	return string(badge.text[:n])
}

// Stable deterministic ordering: z asc, insertion order asc.
layer_outputs_stable_sort :: proc(out: ^Layer_Outputs) {
	if out == nil do return
	if out.count <= 1 do return
	for i in 1 ..< out.count {
		key := out.items[i]
		j := i - 1
		for j >= 0 {
			lhs := out.items[j]
			if lhs.z < key.z do break
			if lhs.z == key.z && lhs.order <= key.order do break
			out.items[j + 1] = lhs
			j -= 1
		}
		out.items[j + 1] = key
	}
}
