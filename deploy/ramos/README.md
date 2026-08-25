# Rack-auto RAMOS
#
# 内存 Alpine：内核 / initramfs / 本机 APK 仓库 / Agent overlay。
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
#   data/ramos/<arch>/vmlinuz-lts initramfs-lts modloop-lts
#   data/ramos/alpine/v3.21/{main,community}/<arch>/
