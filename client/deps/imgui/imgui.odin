package imgui

// Minimal cimgui bindings — core + drawlist.
// Covers only the ~25 procs needed for RCL rendering.
// Verified against cimgui 1.91.8 headers.

import "core:c"

// --- Types ---

Vec2 :: [2]f32
Vec4 :: [4]f32

DrawList :: struct {} // opaque
DrawData :: struct {} // opaque
Context  :: struct {} // opaque
FontAtlas :: struct {} // opaque
Font :: struct {} // opaque

// Partial IO struct — layout matches cimgui up to ini_filename.
// Fields beyond this point are NOT mapped; do not take sizeof(IO).
IO :: struct {
	config_flags:    c.int,
	backend_flags:   c.int,
	display_size:    Vec2,
	delta_time:      f32,
	ini_saving_rate: f32,
	ini_filename:    cstring,
	log_filename:    cstring,
	user_data:       rawptr,
	fonts:           ^FontAtlas,
	font_global_scale: f32,
	font_allow_user_scaling: bool,
	font_default:    ^Font,
	display_framebuffer_scale: Vec2,
}

DrawFlags :: c.int

// --- Foreign import ---

@(extra_linker_flags = "-framework OpenGL")
foreign import cimgui {
	"lib/libcimgui.a",
	"system:c++",
}

// --- Core lifecycle ---

@(default_calling_convention = "c")
foreign cimgui {
	@(link_name = "igCreateContext")
	CreateContext :: proc(shared_font_atlas: ^FontAtlas = nil) -> ^Context ---

	@(link_name = "igDestroyContext")
	DestroyContext :: proc(ctx: ^Context = nil) ---

	@(link_name = "igGetIO")
	GetIO :: proc() -> ^IO ---

	@(link_name = "igNewFrame")
	NewFrame :: proc() ---

	@(link_name = "igRender")
	Render :: proc() ---

	@(link_name = "igGetDrawData")
	GetDrawData :: proc() -> ^DrawData ---

	@(link_name = "igStyleColorsDark")
	StyleColorsDark :: proc(dst: rawptr = nil) ---

	// --- DrawList access ---

	@(link_name = "igGetBackgroundDrawList_Nil")
	GetBackgroundDrawList :: proc() -> ^DrawList ---

	@(link_name = "igGetForegroundDrawList_Nil")
	GetForegroundDrawList :: proc() -> ^DrawList ---

	// --- Color conversion ---

	@(link_name = "igColorConvertFloat4ToU32")
	ColorConvertFloat4ToU32 :: proc(in_: Vec4) -> u32 ---

	// --- DrawList rendering ---

	@(link_name = "ImDrawList_AddRectFilled")
	DrawList_AddRectFilled :: proc(
		self: ^DrawList, p_min, p_max: Vec2, col: u32,
		rounding: f32 = 0, flags: DrawFlags = 0,
	) ---

	@(link_name = "ImDrawList_AddRect")
	DrawList_AddRect :: proc(
		self: ^DrawList, p_min, p_max: Vec2, col: u32,
		rounding: f32 = 0, flags: DrawFlags = 0, thickness: f32 = 1,
	) ---

	@(link_name = "ImDrawList_AddLine")
	DrawList_AddLine :: proc(
		self: ^DrawList, p1, p2: Vec2, col: u32, thickness: f32 = 1,
	) ---

	@(link_name = "ImDrawList_AddText_Vec2")
	DrawList_AddText :: proc(
		self: ^DrawList, pos: Vec2, col: u32,
		text_begin: cstring, text_end: cstring = nil,
	) ---

	@(link_name = "ImDrawList_PushClipRect")
	DrawList_PushClipRect :: proc(
		self: ^DrawList, clip_rect_min, clip_rect_max: Vec2,
		intersect_with_current: bool = true,
	) ---

	@(link_name = "ImDrawList_PopClipRect")
	DrawList_PopClipRect :: proc(self: ^DrawList) ---

	// --- Text measurement ---

	@(link_name = "igCalcTextSize")
	CalcTextSize :: proc(
		pOut: ^Vec2, text: cstring, text_end: cstring = nil,
		hide_text_after_double_hash: bool = false, wrap_width: f32 = -1,
	) ---

	@(link_name = "igGetFontSize")
	GetFontSize :: proc() -> f32 ---

	@(link_name = "igGetTextLineHeight")
	GetTextLineHeight :: proc() -> f32 ---

	// --- Font atlas ---

	@(link_name = "ImFontAtlas_AddFontFromMemoryTTF")
	FontAtlas_AddFontFromMemoryTTF :: proc(
		self: ^FontAtlas, font_data: rawptr, font_data_size: c.int,
		size_pixels: f32, font_cfg: rawptr = nil, glyph_ranges: [^]u16 = nil,
	) -> ^Font ---

	@(link_name = "ImFontAtlas_Build")
	FontAtlas_Build :: proc(self: ^FontAtlas) -> bool ---

	@(link_name = "igPushFont")
	PushFont :: proc(font: ^Font) ---

	@(link_name = "igPopFont")
	PopFont :: proc() ---

	@(link_name = "igGetFont")
	GetFont :: proc() -> ^Font ---
}
