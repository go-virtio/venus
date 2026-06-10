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
  encoders **and decoders** by walking struct/command members exactly as
  Mesa's Python generator does (honouring `optional` / `len` / `altlen` /
  `sType` / `pNext`, enums→int32, handles, `VkBool32`/`VkDeviceSize`/`size_t`,
  nested-by-value structs, fixed-size arrays, the `VkClearColorValue` union,
  counted struct/scalar/**handle/VkFlags** arrays, **typed `pNext` extension
  chains**, **count+array reply decoders**, and **returned-only struct
  decoders**).
- `cmd/vkgen` — the generator CLI (`-xml`, `-out`, `-pkg`).
- `proof` — a generated proof subset spanning the clear-image command closure
  (`vkCreateInstance/Device/Image`, `vkAllocateMemory`, `vkCreateCommandPool`,
  `vkEnumeratePhysicalDevices`, `vkCmdClearColorImage`, `vkQueueSubmit`,
  `vkCmdPipelineBarrier`, plus the `VkClearColorValue` union, the
  `VkSubmitInfo` / `VkImageMemoryBarrier` `pNext`-chain encoders, the
  `vkEnumeratePhysicalDevices` count+array reply decoder, and the
  `VkMemoryRequirements` / `VkPhysicalDeviceProperties` returned-struct
  decoders) whose encoded/decoded bytes are asserted against independently
  hand-derived Mesa bytes.

All five packages are at 100% statement coverage; `CGO_ENABLED=0 go build`
clean; zero external dependencies.

> **Note:** this is the go-virtio family's only non-100%-confidence area in the
> sense that everything beyond the encoder is unbuilt. M0 proves the encoder
> path is mechanical and verifiable. What it does **not** do is just as
> important to state plainly (below).

## What is NOT here yet

- **The full encoder closure** (~120–200 structs/commands) for a real
  "create instance → device → image → clear → readback" path. The clear-image
  *core* — including the submit/barrier framing and the count/readback decode
  rungs below — now encodes/decodes; what remains is breadth (the long tail of
  structs/commands) plus the few generator features still stopped at a clean
  boundary (below).
- **Generator rungs now closed** (encoded/decoded bytes asserted against
  independently hand-derived Mesa bytes):
  - **`pNext` extension chains** — a typed, sType-keyed chain encoder
    (`vncs.EncodePNextChain` + generated `<Struct>Node` constructors)
    transcribed from Mesa's `vn_encode_<Struct>_pnext` walk, including the
    recursion-before-self nesting. Proven on `VkSubmitInfo` (1-node
    `VkProtectedSubmitInfo` chain) and `VkImageMemoryBarrier`.
  - **Count+array reply decoders** — `vkEnumeratePhysicalDevices`-style replies
    (`simple_pointer` out-count + a peeked counted handle array, with the
    `vn_decode_array_size_unchecked` empty arm).
  - **Returned-only struct decoders** — `VkMemoryRequirements` and the full
    `VkPhysicalDeviceProperties` decode (enum, `char[N]`/`uint8[N]` fixed
    arrays, `size_t`, nested `VkPhysicalDeviceLimits`/`SparseProperties`).
  - **`vkQueueSubmit` / `vkCmdPipelineBarrier` framing** — `VkSubmitInfo`
    (`pNext` chain + counted handle/`VkFlags` array members) and the
    three-barrier-group pipeline-barrier command.
- **Generator gaps still open**, each stopped at a clean boundary rather than
  guessed:
  - **The full `VkPhysicalDeviceLimits` (all ~106 members).** The decode
    *generator* handles every member *shape* the real struct uses, proven on a
    curated representative slice (uint32 / `VkDeviceSize` / fixed `uint32[N]` /
    `float` / `size_t` / `int32` / `VkFlags` / `VkBool32` / fixed `float[N]`).
    The remaining members are mechanically identical; they are left out only to
    keep the hand-derived byte stream auditable, not because of any ambiguity.
  - **Unions beyond the single-selector numeric shape** — only the
    `VkClearColorValue`-style 4-element float/int32/uint32 union is emitted;
    pointer-bearing or multi-shape unions are not (no cleanly-derivable case in
    the clear-image path).
  - **The accepted-sType *membership* per parent chain.** The chain *encoding*
    is faithful, but which extension structs a given parent accepts is Mesa's
    per-parent generated switch (not in `vk.xml`); the generator takes the
    accepted node set as an explicit input rather than reconstructing that
    switch.
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
