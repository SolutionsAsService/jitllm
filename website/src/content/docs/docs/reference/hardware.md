---
title: Hardware support
description: The operating systems, CPU instruction sets and GPU backends jitllm generates code for.
---

jitllm has no precompiled kernels. It probes the machine at startup and generates every kernel for what it finds, so the question is not which binary to download but which code generator your hardware gets. `jitllm hardware` prints the answer for the machine in front of you.

## Operating systems

| OS | x86-64 | ARM64 |
|---|:---:|:---:|
| Linux | ✓ | ✓ |
| macOS | builds, untested | ✓ (Apple silicon, with Metal) |
| Windows | ✓ | untested |

## CPUs

| Tier | Requires | What is generated |
|---|---|---|
| AVX2 | AVX2, FMA and F16C (Intel Haswell, AMD Zen and later) | 256-bit VEX code. Integer dot products use VNNI (`VPDPBUSD`) where the CPU has AVX-VNNI, and a three-instruction sequence where it does not. |
| SSE | SSE4.1 and SSSE3 (for example Intel Atom Goldmont) | 128-bit legacy-encoded code with no VEX, FMA or F16C, for CPUs without AVX2 |
| NEON | ARMv8-A with dot-product instructions (Apple M-series and other ARMv8.2 cores) | A64 NEON code with `SDOT` integer dot products |

A CPU below the SSE tier is refused at load with the missing extensions named, rather than crashing on an unsupported instruction. AVX-512 is not used yet; on a Xeon D-2123IT without VNNI, jitllm's 256-bit code decoded Llama-3.2-1B at 97% of llama.cpp's AVX-512 build.

On the CPU too, a prompt is prefilled a chunk of rows at a time as matrix products, including the input and output projections of the recurrent blocks of hybrid and state-space models (the gated delta rule, Kimi Delta Attention, Mamba and the short convolution); only the recurrence itself runs row by row.

Decode uses one worker per physical performance core. Hybrid CPUs' efficiency cores and hyperthread siblings are left out, because decode is bound by memory bandwidth and they add stragglers rather than bandwidth.

## GPUs

| Backend | Code | Platforms | Tested on |
|---|---|---|---|
| CUDA | PTX, loaded by the driver | Linux, WSL, Windows | Tesla V100 (sm_70, up to 5 per model), RTX 3050 Ti Laptop |
| Vulkan | SPIR-V compute | Linux, Windows, macOS through MoltenVK | Intel Iris Xe, RTX 3050 Ti |
| Metal | Metal Shading Language | macOS | Apple M4 |
| HIP (`hip:N`) | AMDGPU code, assembled through ROCm's comgr | Linux and Windows, when ROCm is installed | not yet tested on AMD hardware; an AMD GPU also runs through Vulkan with only its driver |

CUDA, Vulkan and Metal run the same model features: every architecture's block runs whole on the device, including attention, mixture-of-experts routing, multi-head latent attention, the gated delta rule of hybrid models, and vision-tower blocks. Prompts are prefilled on the device in chunks, as matrix products. On CUDA, prefill uses the tensor cores (binary16 by default from sm_80), and decode replays captured graphs.

The device tuners' answers are kept in the user cache directory (`jitllm` under it), keyed by the device's UUID, the driver and the executable, so a second run starts tuned. A shape not measured yet runs a default split while the tuner measures it in the background.

jitllm reaches the drivers through a foreign-function interface rather than cgo, so the same executable runs with or without a GPU and needs no SDK at build time.

## Memory

- **Discrete GPUs** get a weight budget from their free memory, less headroom. See [Devices and placement](/docs/guides/devices/#how-much-memory-a-device-gets).
- **Integrated GPUs and Apple silicon** share system memory, and jitllm counts that memory once.
- **Storage** matters when a model is larger than RAM: the pager reads with parallel direct I/O, so a fast NVMe drive sets the speed. See [Memory and paging](/docs/guides/memory/).
