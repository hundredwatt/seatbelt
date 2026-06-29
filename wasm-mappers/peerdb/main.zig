// Reference WASM mapper: PeerDB row → canonical "common string".
//
// Implements the seatbelt mapper ABI v1 (see ../ABI.md). The host uploads the
// column-family schema once via set_schema, then calls transform_source /
// transform_target once per row. Input is a JSON array of cell values; output
// is the raw common string (NOT JSON-wrapped).
//
// Built as a wasm32-wasi executable with _start skipped by the host (it does no
// I/O); this avoids the PIC issues of a freestanding shared library while still
// exporting plain functions.

const std = @import("std");

const ABI_VERSION: u32 = 1;

const MAX_COLS: usize = 64;
const FAM_STORE_SIZE: usize = 4096;
const OUTPUT_SIZE: usize = 4 * 1024 * 1024; // 4 MiB
const NORM_OUT_SIZE: usize = 1 * 1024 * 1024;
const ERR_SIZE: usize = 1024;

// General-purpose allocator backed by WASM linear memory. Used both for the
// host-facing alloc/dealloc exports and for transient JSON parsing.
const gpa = std.heap.page_allocator;

// Schema storage (set once, reused for every row).
var fam_store: [FAM_STORE_SIZE]u8 = undefined;
var fam_used: usize = 0;
var src_fam: [MAX_COLS][]const u8 = undefined;
var tgt_fam: [MAX_COLS][]const u8 = undefined;
var n_src: usize = 0;
var n_tgt: usize = 0;

// Output buffers. The result of a transform lives in output_buf and is valid
// until the next guest call (the host reads it immediately).
var output_buf: [OUTPUT_SIZE]u8 = undefined;
var norm_out_buf: [NORM_OUT_SIZE]u8 = undefined;

// Last-error channel.
var err_buf: [ERR_SIZE]u8 = undefined;
var err_len: usize = 0;

// ── ABI v1 exports ─────────────────────────────────────────────────────────────

export fn abi_version() u32 {
    return ABI_VERSION;
}

/// Allocate `len` bytes of guest memory and return a pointer the host can write
/// to. Returns 0 on failure.
export fn alloc(len: u32) u32 {
    const slice = gpa.alloc(u8, len) catch return 0;
    return @intCast(@intFromPtr(slice.ptr));
}

/// Release a region previously returned by alloc.
export fn dealloc(ptr: u32, len: u32) void {
    if (ptr == 0 or len == 0) return;
    const p: [*]u8 = @ptrFromInt(@as(usize, ptr));
    gpa.free(p[0..len]);
}

/// last_error returns packed (ptr<<32 | len) of the most recent UTF-8 error
/// message. Valid until the next guest call.
export fn last_error() u64 {
    return pack(@intCast(@intFromPtr(&err_buf[0])), @intCast(err_len));
}

/// Parse {"source_families":[...],"target_families":[...]} from [ptr,len].
/// Returns 0 on success, -1 on error.
export fn set_schema(ptr: u32, len: u32) i32 {
    const input = byteSlice(ptr, len);
    const parsed = std.json.parseFromSlice(std.json.Value, gpa, input, .{}) catch {
        setErr("set_schema: invalid JSON");
        return -1;
    };
    defer parsed.deinit();

    const root = switch (parsed.value) {
        .object => |o| o,
        else => {
            setErr("set_schema: expected object");
            return -1;
        },
    };

    fam_used = 0;
    n_src = 0;
    n_tgt = 0;

    n_src = loadFamilies(root, "source_families", &src_fam) catch return -1;
    n_tgt = loadFamilies(root, "target_families", &tgt_fam) catch return -1;
    return 0;
}

/// Input [ptr,len]: JSON array of cell values (strings/nulls for the source side).
/// Output: raw common string in output_buf. Returns packed (ptr<<32 | len), or 0
/// on error (see last_error).
export fn transform_source(ptr: u32, len: u32) u64 {
    return transformRow(byteSlice(ptr, len), .source);
}

/// Input [ptr,len]: JSON array of cell values (any JSON type for the target side).
/// Output: raw common string in output_buf. Returns packed (ptr<<32 | len), or 0
/// on error (see last_error).
export fn transform_target(ptr: u32, len: u32) u64 {
    return transformRow(byteSlice(ptr, len), .target);
}

// ── core ───────────────────────────────────────────────────────────────────────

const Side = enum { source, target };

fn transformRow(input: []const u8, side: Side) u64 {
    const parsed = std.json.parseFromSlice(std.json.Value, gpa, input, .{}) catch {
        setErr("transform: invalid JSON row");
        return 0;
    };
    defer parsed.deinit();

    const row = switch (parsed.value) {
        .array => |a| a,
        else => {
            setErr("transform: expected array row");
            return 0;
        },
    };

    const n = if (side == .source) n_src else n_tgt;
    if (row.items.len != n) {
        setErr("transform: row length does not match schema");
        return 0;
    }

    var w = std.Io.Writer.fixed(&output_buf);
    for (row.items, 0..) |cell, i| {
        switch (side) {
            .source => transformSourceCell(&w, cell, src_fam[i]) catch {
                setErr("transform_source: write failed");
                return 0;
            },
            .target => {
                const sf = if (i < n_src) src_fam[i] else "";
                transformTargetCell(&w, cell, sf, tgt_fam[i]) catch {
                    setErr("transform_target: write failed");
                    return 0;
                };
            },
        }
    }
    return pack(@intCast(@intFromPtr(&output_buf[0])), @intCast(w.buffered().len));
}

// ── source-side transforms ──────────────────────────────────────────────────────

fn transformSourceCell(w: *std.Io.Writer, cell: std.json.Value, family: []const u8) !void {
    switch (cell) {
        .null => try w.writeByte('0'),
        .string => |s| {
            if (eql(family, "float")) {
                try transformFloatString(w, s);
            } else if (eql(family, "decimal")) {
                try transformDecimalString(w, s);
            } else if (eql(family, "datetime")) {
                try transformDatetimeString(w, s);
            } else if (eql(family, "json")) {
                try w.writeAll(normalizeJson(s));
            } else {
                try w.writeAll(s);
            }
        },
        else => {},
    }
}

fn transformFloatString(w: *std.Io.Writer, s: []const u8) !void {
    if (eql(s, "Infinity")) {
        try w.writeAll("inf");
    } else if (eql(s, "-Infinity")) {
        try w.writeAll("-inf");
    } else if (eql(s, "NaN")) {
        try w.writeAll("nan");
    } else {
        var i: usize = 0;
        while (i < s.len) {
            if (i + 1 < s.len and s[i] == 'e' and s[i + 1] == '+') {
                try w.writeByte('e');
                i += 2;
            } else {
                try w.writeByte(s[i]);
                i += 1;
            }
        }
    }
}

fn transformDecimalString(w: *std.Io.Writer, s: []const u8) !void {
    if (std.mem.indexOf(u8, s, ".") == null) {
        try w.writeAll(s);
        return;
    }
    var end = s.len;
    while (end > 0 and s[end - 1] == '0') end -= 1;
    while (end > 0 and s[end - 1] == '.') end -= 1;
    try w.writeAll(s[0..end]);
}

fn transformDatetimeString(w: *std.Io.Writer, s: []const u8) !void {
    if (s.len == 19 and s[4] == '-' and s[7] == '-' and s[10] == ' ' and s[13] == ':' and s[16] == ':') {
        try w.writeAll(s);
        try w.writeAll(".000000");
    } else {
        try w.writeAll(s);
    }
}

// ── target-side transforms ──────────────────────────────────────────────────────

fn transformTargetCell(
    w: *std.Io.Writer,
    cell: std.json.Value,
    src_family: []const u8,
    tgt_family: []const u8,
) !void {
    if (cell == .null) {
        try w.writeByte('0');
        return;
    }

    if (eql(src_family, "json")) {
        const s = switch (cell) {
            .string => |v| v,
            else => "",
        };
        try w.writeAll(normalizeJson(s));
        return;
    }

    if (eql(tgt_family, "float") or eql(tgt_family, "decimal")) {
        const f: f64 = switch (cell) {
            .float => |v| v,
            .integer => |v| @as(f64, @floatFromInt(v)),
            else => 0.0,
        };
        try w.print("{d:.6}", .{f});
        return;
    }

    if (eql(tgt_family, "integer")) {
        const i: i64 = switch (cell) {
            .integer => |v| v,
            .float => |v| @as(i64, @intFromFloat(v)),
            else => 0,
        };
        try w.print("{d}", .{i});
        return;
    }

    switch (cell) {
        .string => |v| try w.writeAll(v),
        .bool => |v| try w.writeAll(if (v) "true" else "false"),
        .integer => |v| try w.print("{d}", .{v}),
        .float => |v| try w.print("{}", .{v}),
        else => {},
    }
}

// ── JSON normalisation (sorted object keys) ─────────────────────────────────────

fn normalizeJson(s: []const u8) []const u8 {
    if (s.len == 0) return s;

    const parsed = std.json.parseFromSlice(std.json.Value, gpa, s, .{}) catch return s;
    defer parsed.deinit();

    var w = std.Io.Writer.fixed(&norm_out_buf);
    writeJsonValueSorted(&w, parsed.value) catch return s;
    return w.buffered();
}

fn writeJsonValueSorted(w: *std.Io.Writer, val: std.json.Value) !void {
    switch (val) {
        .object => |obj| {
            const keys = obj.keys();
            const sorted = try gpa.dupe([]const u8, keys);
            defer gpa.free(sorted);
            std.sort.heap([]const u8, sorted, {}, struct {
                fn lt(_: void, a: []const u8, b: []const u8) bool {
                    return std.mem.lessThan(u8, a, b);
                }
            }.lt);

            try w.writeByte('{');
            for (sorted, 0..) |key, i| {
                if (i > 0) try w.writeByte(',');
                try writeJsonString(w, key);
                try w.writeByte(':');
                try writeJsonValueSorted(w, obj.get(key).?);
            }
            try w.writeByte('}');
        },
        .array => |arr| {
            try w.writeByte('[');
            for (arr.items, 0..) |item, i| {
                if (i > 0) try w.writeByte(',');
                try writeJsonValueSorted(w, item);
            }
            try w.writeByte(']');
        },
        else => try std.json.Stringify.value(val, .{}, w),
    }
}

fn writeJsonString(w: *std.Io.Writer, s: []const u8) !void {
    try w.writeByte('"');
    for (s) |c| {
        switch (c) {
            '"' => try w.writeAll("\\\""),
            '\\' => try w.writeAll("\\\\"),
            '\n' => try w.writeAll("\\n"),
            '\r' => try w.writeAll("\\r"),
            '\t' => try w.writeAll("\\t"),
            0x00...0x08, 0x0b...0x0c, 0x0e...0x1f => try w.print("\\u{x:0>4}", .{c}),
            else => try w.writeByte(c),
        }
    }
    try w.writeByte('"');
}

// ── helpers ─────────────────────────────────────────────────────────────────────

fn loadFamilies(root: std.json.ObjectMap, key: []const u8, dst: *[MAX_COLS][]const u8) !usize {
    const node = root.get(key) orelse {
        setErr("set_schema: missing families key");
        return error.Bad;
    };
    const arr = switch (node) {
        .array => |a| a,
        else => {
            setErr("set_schema: families not an array");
            return error.Bad;
        },
    };
    var count: usize = 0;
    for (arr.items) |item| {
        const s = switch (item) {
            .string => |v| v,
            else => {
                setErr("set_schema: family not a string");
                return error.Bad;
            },
        };
        const end = fam_used + s.len;
        if (end > fam_store.len or count >= MAX_COLS) {
            setErr("set_schema: schema too large");
            return error.Bad;
        }
        @memcpy(fam_store[fam_used..end], s);
        dst[count] = fam_store[fam_used..end];
        fam_used = end;
        count += 1;
    }
    return count;
}

inline fn byteSlice(ptr: u32, len: u32) []const u8 {
    const p: [*]const u8 = @ptrFromInt(@as(usize, ptr));
    return p[0..len];
}

inline fn pack(ptr: u32, len: u32) u64 {
    return (@as(u64, ptr) << 32) | @as(u64, len);
}

fn setErr(msg: []const u8) void {
    const n = @min(msg.len, err_buf.len);
    @memcpy(err_buf[0..n], msg[0..n]);
    err_len = n;
}

inline fn eql(a: []const u8, b: []const u8) bool {
    return std.mem.eql(u8, a, b);
}

pub fn main() void {}
