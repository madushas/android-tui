# 2. rootAVD runs against our own backup of the stock ramdisk
Status: accepted

rootAVD (third party, interactive, not bundled) patches the ramdisk inside the SDK system image, which every AVD on that image shares. android-tui takes a one-time copy of the stock `ramdisk.img`, runs the script on the real terminal, then judges success by hashing the ramdisk. Restore uses **our** copy, so undoing never depends on rootAVD behaving as we assume.
Unverified assumption (tracked): rootAVD accepts a path relative to the SDK root with that directory as working directory.
