# NCCL tests (all_reduce_perf and friends) for the nccl-tests job template.
# Built on CUDA 13 so that Blackwell (sm_120) GPUs get native kernels; the
# nodes run driver 580, which supports CUDA 13. Single-process mode only:
# no MPI or SSH, each worker tests the GPUs of its own node.
ARG CUDA_IMAGE=nvidia/cuda:13.0.2-devel-ubuntu22.04
FROM ${CUDA_IMAGE}

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
 && rm -rf /opt/nccl-tests/src /opt/nccl-tests/.git

WORKDIR /opt/nccl-tests/build
