# Rack-auto RAMOS
#
# 内存 Ubuntu 26.04.1 LTS live-server：控制面缓存完整 ISO，
# PXE 客户端只拉 casper squashfs 打成的 casper.iso，再由 initrd overlay 拉起 Agent。
# iPXE 固件打在控制面里，PXE 不访问 boot.ipxe.org。
# 完整步骤见 docs/deploy.md。
#
# 有网执行一次：
#   rackauto bootstrap -config configs/rackauto.yaml
# 之后可：
#   rackauto bootstrap -offline
#
# 离线拷贝整个 data/ 目录即可，包括：
#   data/tftp/*.kpxe *.efi
#   data/ramos/ubuntu/<arch>/live-server.iso vmlinuz initrd casper.iso layerfs-path
#
# 待装机机器建议内存 ≥ 4GB（只把 casper.iso 拉进内存，不是 2.7GB 整包）。
