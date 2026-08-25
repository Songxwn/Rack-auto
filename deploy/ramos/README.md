# Rack-auto RAMOS
#
# 内存中的 Alpine：内核 / initramfs / Agent overlay。
# 完整部署步骤见 docs/deploy.md。
#
# 在控制面执行一次：
#   rackauto bootstrap -config configs/rackauto.yaml
# 会下载 Alpine 到 data/ramos/<arch>/，并准备 Linux Agent。
#
# 完全离线时，把 Alpine netboot 文件放到 data/ramos/<arch>/，
# 把 iPXE 文件放到 data/tftp/，并把 public_url 指到控制面。
