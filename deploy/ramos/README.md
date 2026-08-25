# Rack-auto RAMOS
#
# 内存 Ubuntu 26.04 LTS live-server：ISO + casper 内核 + autoinstall 拉起 Agent。
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
#   data/ramos/ubuntu/<arch>/live-server.iso vmlinuz initrd
#
# 待装机机器建议内存 ≥ 8GB（casper 会把 ISO 拉进内存）。
