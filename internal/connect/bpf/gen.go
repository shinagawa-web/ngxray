// Package bpf holds the bpf2go-generated bindings for the kernel-side
// program in connect.bpf.c. Regenerate with `go generate` after editing
// the C source (requires clang and libbpf-dev on Linux).
package bpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -output-dir . -go-package bpf Connect ./connect.bpf.c -- -I/usr/include/aarch64-linux-gnu -I/usr/include/x86_64-linux-gnu
