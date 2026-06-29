const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.resolveTargetQuery(.{
        .cpu_arch = .wasm32,
        .os_tag = .wasi,
    });
    const mod = b.createModule(.{
        .root_source_file = b.path("main.zig"),
        .target = target,
        .optimize = .ReleaseFast,
    });
    const exe = b.addExecutable(.{
        .name = "peerdb",
        .root_module = mod,
    });
    // Keep exported functions (alloc/dealloc/transform_*/…) in the final binary.
    exe.rdynamic = true;
    b.installArtifact(exe);
}
