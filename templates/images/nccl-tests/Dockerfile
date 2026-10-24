# NCCL tests (all_reduce_perf and friends) for the nccl-tests job template.
# Built on CUDA 13 so that Blackwell (sm_120) GPUs get native kernels; the
# nodes run driver 580, which supports CUDA 13. Single-process mode only:
# no MPI or SSH, each worker tests the GPUs of its own node.
#
# Two stages keep the image small: the binaries are compiled on the devel
# image (compiler, headers, NCCL development files) and run on the base image
# with only the CUDA runtime and NCCL libraries, so nodes pull about a tenth
# of the devel image.
ARG CUDA_VERSION=13.0.2
ARG UBUNTU=ubuntu22.04

FROM nvidia/cuda:${CUDA_VERSION}-devel-${UBUNTU} AS build

ARG NCCL_TESTS_REF=v2.21.1

# The devel image already ships NCCL (libnccl-dev, libnccl2) and holds those
# packages at its version, so only git is installed here.
RUN apt-get update \
 && apt-get install -y --no-install-recommends git ca-certificates \
 && rm -rf /var/lib/apt/lists/*

RUN git clone --depth 1 --branch "${NCCL_TESTS_REF}" https://github.com/NVIDIA/nccl-tests.git /opt/nccl-tests \
 && make -C /opt/nccl-tests -j"$(nproc)" \
      CUDA_HOME=/usr/local/cuda NCCL_HOME=/usr MPI=0 \
      NVCC_GENCODE="-gencode=arch=compute_80,code=sm_80 -gencode=arch=compute_90,code=sm_90 -gencode=arch=compute_100,code=sm_100 -gencode=arch=compute_120,code=sm_120" \
 && rm -rf /opt/nccl-tests/build/*.o /opt/nccl-tests/build/verifiable

FROM nvidia/cuda:${CUDA_VERSION}-base-${UBUNTU}

# The runtime libraries the binaries link against. The package names follow
# the CUDA major.minor of the base image; the NCCL package comes from the same
# NVIDIA repository the base image configures. The apt lists stay out of the
# layer.
ARG CUDA_PACKAGE_SUFFIX=13-0
RUN apt-get update \
 && apt-get install -y --no-install-recommends cuda-cudart-${CUDA_PACKAGE_SUFFIX} libnccl2 \
 && rm -rf /var/lib/apt/lists/*

COPY --from=build /opt/nccl-tests/build /opt/nccl-tests/build

WORKDIR /opt/nccl-tests/build
