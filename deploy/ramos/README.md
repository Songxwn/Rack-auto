# Rack-auto RAMOS
#
# 推荐用法：在控制面主机执行
#   rackauto bootstrap
# 会下载 Alpine 内核/initramfs/modloop，并交叉编译 Linux Agent。
# 机器 iPXE 启动后进入内存中的 Alpine，加载 apkovl 并运行 Agent。
#
# 若需完全离线，把 Alpine 仓库镜像到 data/ramos/alpine/<arch>/ 并把
# iPXE 脚本中的 alpine_repo 指向控制面。
