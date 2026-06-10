# go-virtio/venus

Pure-Go (CGO=0) groundwork for **Venus** — the Vulkan-over-virtio protocol —
as a guest-side counterpart to the [`go-virtio`](https://github.com/go-virtio)
virtio-gpu work. Vulkan-in-Go is a genuinely larger undertaking than the
virgl/GL path: Venus reuses the virtio-gpu *device* but replaces the whole
submission model and serialises essentially the entire Vulkan API. This repo
de-risks it bottom-up, one verifiable rung at a time.

## Status — Milestone 0 (encoder generator)

M0 is the **offline-verifiable** part: a `vk.xml`→Go generator that emits Venus
wire serialisers, with the output checked **byte-for-byte against bytes derived
from Mesa's generated headers — no GPU, no host required.**

- `internal/vncs` — the wire runtime (the `vn_cs.h` equivalent): an `Encoder`
  and a mirroring `Decoder` over a `[]byte` with LE, 4-byte-aligned
  primitives — `Encode/Decode` `Uint32/Int32/Uint64/Float32/Flags/Bool32/
  DeviceSize/ArraySize/SimplePointer/BlobArray/String/Handle/Result` plus the
  fixed/union array helpers (`Uint8Array/Uint32Array/Int32Array/Float32Array`)
  and `PeekArraySize`. Each method transcribes a specific Mesa
  `vn_encode_*` / `vn_decode_*` function (cited inline).
- `gen` — parses `vk.xml` (`encoding/xml`) into a typed model and emits Go
  encoders **and reply decoders** by walking struct/command members exactly as
  Mesa's Python generator does (honouring `optional` / `len` / `altlen` /
  `sType` / `pNext`, enums→int32, handles, `VkBool32`/`VkDeviceSize`,
  nested-by-value structs, fixed-size arrays, the `VkClearColorValue` union,
  and counted struct/scalar arrays).
- `cmd/vkgen` — the generator CLI (`-xml`, `-out`, `-pkg`).
- `proof` — a generated proof subset spanning the clear-image command closure
  (`vkCreateInstance/Device/Image`, `vkAllocateMemory`, `vkCreateCommandPool`,
  `vkEnumeratePhysicalDevices`, `vkCmdClearColorImage`, plus the
  `VkClearColorValue` union and `Decode_*_reply` decoders) whose encoded/decoded
  bytes are asserted against independently hand-derived Mesa bytes.

All four packages are at 100% statement coverage; `CGO_ENABLED=0 go build`
clean; zero external dependencies.

> **Note:** this is the go-virtio family's only non-100%-confidence area in the
> sense that everything beyond the encoder is unbuilt. M0 proves the encoder
> path is mechanical and verifiable. What it does **not** do is just as
> important to state plainly (below).

## What is NOT here yet

- **The full encoder closure** (~120–200 structs/commands) for a real
  "create instance → device → image → clear → readback" path. The clear-image
  *core* now encodes/decodes, but several commands still need generator
  features the proof set deliberately stops short of (below).
- **Generator gaps to a complete clear-image path**, each stopped at a clean
  boundary rather than guessed:
  - **`pNext` extension chains** — every struct emits `pNext = NULL`. A typed
    `pNext` switch (sType-keyed) is unimplemented.
  - **Unions beyond the single-selector numeric shape** — only the
    `VkClearColorValue`-style 4-element float/int32/uint32 union is emitted;
    pointer-bearing or multi-shape unions are not.
  - **Non-create reply decoders** — reply decode covers the single-handle
    "create" replies; the count+array reply (e.g.
    `vkEnumeratePhysicalDevices`) and `_partial` returned-only structs
    (`VkPhysicalDeviceProperties`/`Limits`, `VkMemoryRequirements`) are not yet
    emitted on the decode side.
  - **`vkQueueSubmit`/`vkCmdPipelineBarrier` framing** — their `VkSubmitInfo` /
    `VkImageMemoryBarrier` inputs need `pNext` and counted-handle-array members
    the emitter does not yet cover.
- **The transport — the real unknown.** Venus needs a virtio-gpu context with
  `context_init = VIRTIO_GPU_CAPSET_VENUS (4)` (requiring `F_CONTEXT_INIT` +
  `RESOURCE_BLOB`), a guest/host **shared-memory command ring**, `EXECBUFFER`
  kicks, and `drm_syncobj` fencing. None of that is derivable from headers
  alone; it depends on host/renderer behaviour and is where correctness will
  actually be won or lost.

Estimated total effort for a first on-screen Venus result: ~4–8× the virgl
clear-screen milestone. This is a project, not a milestone.

## Build

This module is not part of the parent workspace; build/test with `GOWORK=off`:

```
GOWORK=off go test ./...
GOWORK=off go run ./cmd/vkgen -xml vk.xml -out encoders_gen.go -pkg myvk
```

## License

BSD-3-Clause. See [LICENSE](LICENSE).
