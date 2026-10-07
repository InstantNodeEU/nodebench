#!/usr/bin/env bash
#
# nodebench - cpu, disk and network benchmark for linux servers
# https://github.com/instantnode/nodebench
#
#   curl -sL https://bench.instantnode.eu | bash
#   curl -sL https://bench.instantnode.eu | bash -s -- -n --no-share
#
# Needs bash 4 and curl. No root, nothing gets installed. fio and iperf3 are
# fetched into a temp dir if they are missing and removed again at the end.

VERSION="1.1.0"
NODEBENCH_URL="${NODEBENCH_URL:-https://bench.instantnode.eu}"

# static fio/iperf3 builds published by the yabs project
BIN_URL="https://github.com/masonr/yet-another-bench-script/releases/download"
FIO_TAG="fio-3.43"
IPERF_TAG="iperf3-3.22"

# host|port range|provider|location|region, an x after the region means the
# server only runs with -x
IPERF_SERVERS=(
	"lg.instantnode.eu|5201|InstantNode|Eygelshoven, NL|eu"
	"lon.speedtest.clouvider.net|5200-5209|Clouvider|London, UK|eu"
	"iperf-ams-nl.eranium.net|5201-5210|Eranium|Amsterdam, NL|eu"
	"speedtest.fra1.de.leaseweb.net|5201-5210|Leaseweb|Frankfurt, DE|eu"
	"iperf3.moji.fr|5200-5240|Moji|Paris, FR|eu"
	"speedtest.wtnet.de|5200-5209|wilhelm.tel|Hamburg, DE|eu x"
	"speedtest.nyc1.us.leaseweb.net|5201-5210|Leaseweb|New York, US|na"
	"speedtest.chi11.us.leaseweb.net|5201-5210|Leaseweb|Chicago, US|na x"
	"speedtest.mia11.us.leaseweb.net|5201-5210|Leaseweb|Miami, US|na x"
	"speedtest.mtl2.ca.leaseweb.net|5201-5210|Leaseweb|Montreal, CA|na x"
	"speedtest.dal13.us.leaseweb.net|5201-5210|Leaseweb|Dallas, US|na"
	"la.speedtest.clouvider.net|5200-5209|Clouvider|Los Angeles, US|na"
	"speedtest.sao1.edgoo.net|9204-9240|Edgoo|Sao Paulo, BR|sa"
	"speedtest.sin1.sg.leaseweb.net|5201-5210|Leaseweb|Singapore, SG|asia"
	"speedtest.hkg12.hk.leaseweb.net|5201-5210|Leaseweb|Hong Kong, HK|asia x"
	"speedtest.tyo11.jp.leaseweb.net|5201-5210|Leaseweb|Tokyo, JP|asia"
	"speedtest.syd12.au.leaseweb.net|5201-5210|Leaseweb|Sydney, AU|oc"
)

# download only, used when iperf3 can't be run or every server above failed
HTTP_SERVERS=(
	"https://fsn1-speed.hetzner.com/1GB.bin|Hetzner|Falkenstein, DE"
	"https://hel1-speed.hetzner.com/1GB.bin|Hetzner|Helsinki, FI"
	"https://ash-speed.hetzner.com/1GB.bin|Hetzner|Ashburn, US"
	"https://hil-speed.hetzner.com/1GB.bin|Hetzner|Hillsboro, US"
	"https://sin-speed.hetzner.com/1GB.bin|Hetzner|Singapore, SG"
)

FIO_TIME="${NODEBENCH_FIO_TIME:-20}"
IPERF_TIME="${NODEBENCH_IPERF_TIME:-10}"
CPU_TIME="${NODEBENCH_CPU_TIME:-5}"

usage() {
	cat <<EOF
nodebench $VERSION

usage: nodebench.sh [options]

  -c            skip cpu test
  -s            skip disk test
  -n            skip network test
  -4, -6        only run network tests over IPv4 / IPv6
  -d DIR        directory for the disk test file (default: current dir)
  -S SIZE       size of the disk test file (default: 2G)
  -t N          threads for the multi-core cpu test (default: all)
  -l REGIONS    only test these network regions, comma separated:
                eu, na, sa, asia, oc (default: all)
  -x            test against all locations, not just the standard 12
  -q            quick run: shorter tests and a 512M disk file, for a
                first look (quick runs don't go on the leaderboard)
  -L [NAME]     put the result on the public leaderboard, optionally
                under NAME (default: the provider name)
  --no-share    don't upload the result
  --json        only print the result as json
  -h            this help

Upload target can be changed with NODEBENCH_URL.
EOF
}

# --- output -----------------------------------------------------------------

# colors and box drawing only when stdout is a terminal that can take them;
# NO_COLOR (https://no-color.org) and plain ascii locales get the boring version
style_init() {
	B= D= A= G= Y= R= N= HL='-' BLK='#' EMP='.' SEP='|' SPIN='|/-\' LOGO='[nb]'
	if [[ -t 3 && -z $NO_COLOR && $TERM != dumb ]]; then
		B=$'\033[1m' D=$'\033[2m' N=$'\033[0m'
		if [[ $COLORTERM == *truecolor* || $COLORTERM == *24bit* ]]; then
			A=$'\033[38;2;59;130;246m' G=$'\033[38;2;63;185;80m' Y=$'\033[38;2;227;179;65m' R=$'\033[38;2;248;81;73m'
		else
			A=$'\033[34m' G=$'\033[32m' Y=$'\033[33m' R=$'\033[31m'
		fi
	fi
	if [[ -t 3 && ${LC_ALL:-${LC_CTYPE:-$LANG}} == *[Uu][Tt][Ff]*8* ]]; then
		HL='─' BLK='━' EMP='─' SEP='│' SPIN='⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏' LOGO='▁▃▅▇'
	fi
}

say() { printf '%s\n' "$*" >&3; }

STEP=0
status() {
	[[ -t 3 ]] || return 0
	local c=${SPIN:STEP++ % ${#SPIN}:1}
	printf '\r\033[K  %s%s%s %s%s%s' "$A" "$c" "$N" "$D" "$*" "$N" >&3
}

clear_status() {
	[[ -t 3 ]] && printf '\r\033[K' >&3
}

warn() { printf '  %s!%s %s\n' "$Y" "$N" "$*" >&2; }

rule() {
	local n=${1:-66} line=
	while (( n-- > 0 )); do line+=$HL; done
	printf '%s' "$line"
}

# header <title> [detail]
header() {
	say ""
	say "  $B$1$N${2:+  $D$2$N}"
	say "  $D$(rule)$N"
}

# kv <label> <value>: one aligned line of the system block
kv() { printf '  %s%-12s%s %s\n' "$D" "$1" "$N" "$2" >&3; }

# th <format> <columns...>: a dimmed table header
th() {
	local fmt=$1; shift
	# shellcheck disable=SC2059
	printf "  $D$fmt$N\n" "$@" >&3
}

# bar <value> <max> <width>: filled part in the accent color, rest dimmed
bar() {
	local n
	n=$(awk -v v="$1" -v m="$2" -v w="$3" 'BEGIN {
		n = (m > 0) ? int(v / m * w + 0.5) : 0
		if (v > 0 && n < 1) n = 1
		if (n > w) n = w
		print n }')
	local full= empty= i
	for ((i = 0; i < n; i++)); do full+=$BLK; done
	for ((; i < $3; i++)); do empty+=$EMP; done
	printf '%s%s%s%s%s' "$A" "$full" "$D" "$empty" "$N"
}

# pad <width> <text>: left align text that may contain color codes
pad() {
	local plain
	plain=$(sed $'s/\033\\[[0-9;]*m//g' <<<"$2")
	printf '%s%*s' "$2" $(($1 - ${#plain})) ''
}

# ping_color <ms>: green below 50, yellow below 150, red above
ping_color() {
	local v
	v=$(fmt_ms "$1")
	[[ $v == - ]] && { printf '%s-%s' "$D" "$N"; return; }
	if awk -v p="$1" 'BEGIN { exit !(p < 50) }'; then printf '%s%s%s' "$G" "$v" "$N"
	elif awk -v p="$1" 'BEGIN { exit !(p < 150) }'; then printf '%s%s%s' "$Y" "$v" "$N"
	else printf '%s%s%s' "$R" "$v" "$N"; fi
}

# speed <mbps>: "failed" in red instead of a misleading zero
speed() {
	if awk -v v="$1" 'BEGIN { exit !(v <= 0) }'; then printf '%sfailed%s' "$R" "$N"
	else fmt_mbps "$1"; fi
}

# awk does the float math, bash can't
fmt_bytes() { awk -v v="$1" 'BEGIN { if (v >= 1e9) printf "%.2f GB/s", v/1e9; else printf "%.0f MB/s", v/1e6 }'; }
fmt_kbs() {
	awk -v v="$1" 'BEGIN {
		if (v >= 1048576) printf "%.2f GB/s", v/1048576
		else if (v >= 1024) printf "%.1f MB/s", v/1024
		else printf "%.0f KB/s", v }'
}
fmt_iops() { awk -v v="$1" 'BEGIN { if (v >= 1e6) printf "%.2fM", v/1e6; else if (v >= 1e5) printf "%.0fk", v/1e3; else if (v >= 1e4) printf "%.1fk", v/1e3; else printf "%.0f", v }'; }
fmt_mbps() { awk -v v="$1" 'BEGIN { if (v <= 0) printf "-"; else if (v >= 1000) printf "%.2f Gbit/s", v/1000; else printf "%.0f Mbit/s", v }'; }
fmt_ms() { awk -v v="$1" 'BEGIN { if (v <= 0) printf "-"; else if (v < 10) printf "%.1f ms", v; else printf "%.0f ms", v }'; }
fmt_kib() {
	awk -v v="$1" 'BEGIN {
		split("KiB MiB GiB TiB PiB", u, " "); i = 1
		while (v >= 1024 && i < 5) { v /= 1024; i++ }
		printf "%.1f %s", v, u[i] }'
}
add() { awk -v a="$1" -v b="$2" 'BEGIN { printf "%.2f", a + b }'; }

# --- json -------------------------------------------------------------------

jstr() {
	local s
	s=$(printf '%s' "$1" | tr -d '\000-\037')
	s=${s//\\/\\\\}
	s=${s//\"/\\\"}
	printf '"%s"' "$s"
}

jnum() {
	if [[ $1 =~ ^[0-9]+(\.[0-9]+)?$ ]]; then printf '%s' "$1"; else printf 0; fi
}

jbool() {
	if [[ -n $1 ]]; then printf true; else printf false; fi
}

# --- system info ------------------------------------------------------------

collect_system() {
	SYS_OS="Linux"
	if [[ -r /etc/os-release ]]; then
		# shellcheck disable=SC1091
		SYS_OS=$(. /etc/os-release && echo "${PRETTY_NAME:-$NAME}")
	fi
	SYS_KERNEL=$(uname -r)
	SYS_ARCH=$(uname -m)
	SYS_CORES=$(nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo)

	SYS_CPU=$(awk -F': *' '/^model name/ { print $2; exit }' /proc/cpuinfo)
	if [[ -z $SYS_CPU ]] && command -v lscpu >/dev/null; then
		SYS_CPU=$(lscpu | awk -F': *' '/^Model name/ { print $2; exit }')
	fi
	SYS_CPU=${SYS_CPU:-unknown}
	SYS_MHZ=$(awk -F': *' '/^cpu MHz/ { printf "%.0f", $2; exit }' /proc/cpuinfo)
	if [[ -z $SYS_MHZ && -r /sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq ]]; then
		SYS_MHZ=$(( $(cat /sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq) / 1000 ))
	fi

	SYS_AES=; SYS_VMX=
	grep -qw aes /proc/cpuinfo && SYS_AES=1
	grep -qwE 'vmx|svm' /proc/cpuinfo && SYS_VMX=1

	SYS_VIRT=
	if command -v systemd-detect-virt >/dev/null; then
		SYS_VIRT=$(systemd-detect-virt 2>/dev/null)
	fi
	if [[ -z $SYS_VIRT || $SYS_VIRT == none ]]; then
		if [[ -f /.dockerenv ]]; then
			SYS_VIRT=docker
		elif grep -qa 'container=lxc' /proc/1/environ 2>/dev/null; then
			SYS_VIRT=lxc
		elif [[ -d /proc/vz && ! -d /proc/bc ]]; then
			SYS_VIRT=openvz
		elif grep -qw hypervisor /proc/cpuinfo; then
			SYS_VIRT=$(tr '[:upper:]' '[:lower:]' </sys/class/dmi/id/sys_vendor 2>/dev/null | awk '{print $1}')
			SYS_VIRT=${SYS_VIRT:-vm}
		else
			SYS_VIRT=none
		fi
	fi

	SYS_RAM=$(awk '/^MemTotal/ { print $2 }' /proc/meminfo)
	SYS_SWAP=$(awk '/^SwapTotal/ { print $2 }' /proc/meminfo)

	# sum each block device once, bind mounts and btrfs subvolumes would count double
	SYS_DISK=$(df -k -l -x tmpfs -x devtmpfs -x overlay -x squashfs -x efivarfs --output=source,size 2>/dev/null |
		awk 'NR > 1 && $1 ~ /^\// && !seen[$1]++ { s += $2 } END { print s + 0 }')
	if [[ -z $SYS_DISK || $SYS_DISK == 0 ]]; then
		SYS_DISK=$(df -k -P / | awk 'NR == 2 { print $2 }')
	fi

	SYS_UPTIME=$(awk '{ printf "%d", $1 }' /proc/uptime)

	HAS_V4=; HAS_V6=
	curl -s4 -o /dev/null --max-time 5 https://www.cloudflare.com/cdn-cgi/trace && HAS_V4=1
	curl -s6 -o /dev/null --max-time 5 https://www.cloudflare.com/cdn-cgi/trace && HAS_V6=1

	LOC_ASN=; LOC_ORG=; LOC_COUNTRY=
	local info
	info=$(curl -s --max-time 5 https://ipinfo.io/json)
	if [[ -n $info ]]; then
		local org
		org=$(json_field org <<<"$info")
		if [[ $org == AS* ]]; then
			LOC_ASN=${org%% *}
			LOC_ORG=${org#* }
		else
			LOC_ORG=$org
		fi
		LOC_COUNTRY=$(json_field country <<<"$info")
	fi
	# InstantNode's own network, the servers are in Eygelshoven
	if [[ $LOC_ASN == AS49581 ]]; then
		LOC_ORG=InstantNode LOC_COUNTRY=NL
	fi
}

# good enough for ipinfo's flat, one key per line output
json_field() {
	sed -n "s/^ *\"$1\": *\"\\(.*\\)\",\\{0,1\\}\$/\\1/p" | head -n1
}

print_system() {
	local uptime_d=$((SYS_UPTIME / 86400)) uptime_h=$((SYS_UPTIME % 86400 / 3600)) uptime_m=$((SYS_UPTIME % 3600 / 60))
	local yes="${G}yes$N" no="${D}no$N" on="${G}online$N" off="${D}offline$N"
	header "System"
	kv "Processor" "$B$SYS_CPU$N"
	kv "Cores" "$SYS_CORES${SYS_MHZ:+ @ $SYS_MHZ MHz}"
	kv "AES-NI" "$([[ -n $SYS_AES ]] && echo "$yes" || echo "$no")"
	kv "VM-x/AMD-V" "$([[ -n $SYS_VMX ]] && echo "$yes" || echo "$no")"
	kv "Memory" "$(fmt_kib "$SYS_RAM") RAM, $(fmt_kib "${SYS_SWAP:-0}") swap"
	kv "Disk" "$(fmt_kib "$SYS_DISK")"
	kv "Distro" "$SYS_OS"
	kv "Kernel" "$SYS_KERNEL ($SYS_ARCH)"
	kv "Virt" "$SYS_VIRT"
	kv "Uptime" "${uptime_d}d ${uptime_h}h ${uptime_m}m"
	kv "Network" "IPv4 $([[ -n $HAS_V4 ]] && echo "$on" || echo "$off"), IPv6 $([[ -n $HAS_V6 ]] && echo "$on" || echo "$off")"
	if [[ -n $LOC_ORG ]]; then
		kv "Provider" "${LOC_ASN:+$LOC_ASN }$LOC_ORG${LOC_COUNTRY:+ ($LOC_COUNTRY)}"
	fi
}

# --- helpers for fetched binaries ------------------------------------------

bin_arch() {
	case $(uname -m) in
		x86_64 | amd64) echo x64 ;;
		aarch64 | arm64) echo aarch64 ;;
		armv7* | armv6* | arm) echo arm ;;
		i?86) echo x86 ;;
		*) return 1 ;;
	esac
}

# fetch_bin <tag> <name>: downloads <name>_<arch> into $WORK and checks it
# against the release checksum file when sha256sum is around.
fetch_bin() {
	local tag=$1 name=$2 arch file sums want
	arch=$(bin_arch) || return 1
	file="$WORK/$name"
	curl -sfL --retry 3 --connect-timeout 5 -o "$file" "$BIN_URL/$tag/${name}_$arch" || return 1
	if command -v sha256sum >/dev/null; then
		sums=$(curl -sfL --connect-timeout 5 "$BIN_URL/$tag/$name-checksums.sha256")
		want=$(awk -v f="${name}_$arch" '$2 == f || $2 == "*"f { print $1 }' <<<"$sums")
		if [[ -n $want ]] && [[ $(sha256sum "$file" | awk '{print $1}') != "$want" ]]; then
			warn "checksum mismatch for $name, not using it"
			rm -f "$file"
			return 1
		fi
	fi
	chmod +x "$file"
	"$file" --version >/dev/null 2>&1 || return 1
	echo "$file"
}

# --- cpu --------------------------------------------------------------------

# cpu_ticks prints "steal total" summed over all cpus from /proc/stat
cpu_ticks() {
	awk '/^cpu / { t = 0; for (i = 2; i <= NF; i++) t += $i; print $9 + 0, t; exit }' /proc/stat
}

# ossl_speed <threads> <algorithm>, prints bytes per second
ossl_speed() {
	local multi=()
	(( $1 > 1 )) && multi=(-multi "$1")
	openssl speed -mr "${multi[@]}" -seconds "$CPU_TIME" -bytes 16384 -evp "$2" </dev/null 2>/dev/null |
		grep '^+F:' | tail -n1 | cut -d: -f4
}

cpu_test() {
	if ! command -v openssl >/dev/null; then
		warn "openssl not found, skipping cpu test"
		return
	fi
	CPU_OPENSSL=$(openssl version | awk '{ print $1, $2 }')

	status "cpu: sha256, 1 thread"
	CPU_SHA=$(ossl_speed 1 sha256)
	if [[ -z $CPU_SHA ]]; then
		clear_status
		warn "openssl speed failed (too old?), skipping cpu test"
		return
	fi
	status "cpu: aes-256-gcm, 1 thread"
	CPU_AES=$(ossl_speed 1 aes-256-gcm)
	if (( THREADS > 1 )); then
		status "cpu: sha256, $THREADS threads"
		local before after
		before=$(cpu_ticks)
		CPU_SHA_N=$(ossl_speed "$THREADS" sha256)
		after=$(cpu_ticks)
		# share of time the hypervisor gave our cores to someone else
		CPU_STEAL=$(awk -v a="$before" -v b="$after" 'BEGIN {
			split(a, x, " "); split(b, y, " ")
			d = y[2] - x[2]; if (d <= 0) { print 0; exit }
			printf "%.1f", (y[1] - x[1]) / d * 100 }')
		status "cpu: aes-256-gcm, $THREADS threads"
		CPU_AES_N=$(ossl_speed "$THREADS" aes-256-gcm)
	else
		CPU_SHA_N=$CPU_SHA
		CPU_AES_N=$CPU_AES
	fi
	clear_status
	CPU_DONE=1

	header "CPU" "openssl speed, 16 KiB blocks"
	local name one all
	if (( THREADS > 1 )); then
		th '%-14s %-12s %-12s' "Test" "1 thread" "$THREADS threads"
	else
		th '%-14s %-12s' "Test" "1 thread"
	fi
	for name in sha256 aes-256-gcm; do
		if [[ $name == sha256 ]]; then one=$CPU_SHA all=$CPU_SHA_N; else one=${CPU_AES:-0} all=${CPU_AES_N:-0}; fi
		if (( THREADS > 1 )); then
			printf '  %-14s %-12s %s%-12s%s %s\n' "$name" "$(fmt_bytes "$one")" "$B" "$(fmt_bytes "$all")" "$N" \
				"$(bar "$one" "$all" 24)" >&3
		else
			printf '  %-14s %s%-12s%s\n' "$name" "$B" "$(fmt_bytes "$one")" "$N" >&3
		fi
	done
	if (( THREADS > 1 )); then
		say ""
		kv "Scaling" "$A$(awk -v s="$CPU_SHA" -v n="$CPU_SHA_N" 'BEGIN { if (s > 0) printf "%.1fx", n / s }')$N from 1 to $THREADS threads"
		kv "Steal time" "${CPU_STEAL:-0}% during the $THREADS thread run"
	fi
}

# --- disk -------------------------------------------------------------------

size_kib() {
	local n=${1%[KkMmGg]} unit=${1: -1}
	[[ $n =~ ^[0-9]+$ ]] || return 1
	case $unit in
		G | g) echo $((n * 1024 * 1024)) ;;
		M | m) echo $((n * 1024)) ;;
		K | k) echo "$n" ;;
		*) return 1 ;;
	esac
}

disk_test() {
	local need avail
	if ! need=$(size_kib "$SIZE"); then
		warn "bad test file size: $SIZE"
		return
	fi
	if [[ ! -d $DIR || ! -w $DIR ]]; then
		warn "can't write to $DIR, skipping disk test"
		return
	fi
	avail=$(df -k -P "$DIR" | awk 'NR == 2 { print $4 }')
	if (( avail < need + need / 5 )); then
		warn "not enough free space in $DIR for a $SIZE test file, skipping disk test"
		return
	fi

	TESTFILE="$DIR/nodebench.$$.fio"
	disk_media

	local fio
	fio=$(command -v fio)
	if [[ -z $fio ]]; then
		status "disk: fetching fio"
		fio=$(fetch_bin "$FIO_TAG" fio)
	fi

	if [[ -n $fio ]]; then
		disk_fio "$fio"
	else
		clear_status
		warn "fio not available, falling back to dd (sequential only)"
		disk_dd
	fi
	rm -f "$TESTFILE"
}

# disk_media works out the filesystem and what kind of device sits under
# the test directory. Virtual disks report "rotational" no matter what is
# behind them, so those are just called virtual.
disk_media() {
	local src dev base
	DISK_FS=$(df -T -P "$DIR" 2>/dev/null | awk 'NR == 2 { print $2 }')
	DISK_MEDIA=
	if [[ $DISK_FS == tmpfs || $DISK_FS == ramfs ]]; then
		warn "$DIR is $DISK_FS, so the disk test measures memory. Use -d to point it at a real disk."
	fi
	src=$(df -P "$DIR" 2>/dev/null | awk 'NR == 2 { print $1 }')
	[[ $src == /dev/* ]] || return
	dev=$(readlink -f "$src")
	base=$(lsblk -no PKNAME "$dev" 2>/dev/null | head -n1)
	base=${base:-${dev##*/}}
	case $base in
		nvme*) DISK_MEDIA=nvme ;;
		vd* | xvd*) DISK_MEDIA=virtual ;;
		*)
			if [[ -r /sys/block/$base/queue/rotational ]]; then
				if [[ $(</sys/block/$base/queue/rotational) == 0 ]]; then DISK_MEDIA=ssd; else DISK_MEDIA=hdd; fi
			fi
			;;
	esac
}

disk_fio() {
	local fio=$1 engine=libaio direct=1 bs out
	"$fio" --enghelp 2>/dev/null | grep -qw libaio || engine=psync

	# lay the file out once so the timed runs don't include it
	status "disk: writing $SIZE test file"
	if ! "$fio" --name=setup --ioengine="$engine" --rw=write --bs=1M --size="$SIZE" \
		--direct=1 --filename="$TESTFILE" </dev/null >/dev/null 2>&1; then
		# tmpfs and a few filesystems refuse O_DIRECT
		direct=0
		"$fio" --name=setup --ioengine="$engine" --rw=write --bs=1M --size="$SIZE" \
			--filename="$TESTFILE" </dev/null >/dev/null 2>&1
	fi

	DISK_TOOL=fio
	DISK_ROWS=()
	for bs in 4k 64k 512k 1m; do
		status "disk: random read/write, $bs blocks"
		out=$("$fio" --name=nodebench --ioengine="$engine" --rw=randrw --rwmixread=50 --bs="$bs" \
			--iodepth=64 --numjobs=2 --size="$SIZE" --runtime="$FIO_TIME" --time_based \
			--gtod_reduce=1 --direct="$direct" --filename="$TESTFILE" --group_reporting \
			--output-format=terse </dev/null 2>/dev/null | tail -n1)
		# terse v3: 7/8 = read bw (KiB/s) and iops, 48/49 = write
		[[ -z $out ]] && continue
		DISK_ROWS+=("$(awk -F';' -v bs="$bs" '{ print bs, $7, $48, $8, $49 }' <<<"$out")")
	done
	clear_status
	print_disk "fio, random 50/50 read/write, $SIZE file"
}

disk_dd() {
	local count=$(( $(size_kib "$SIZE") / 1024 )) t0 t1 w r
	DISK_TOOL="dd"
	DISK_ROWS=()

	status "disk: dd sequential write"
	t0=$(date +%s%N)
	dd if=/dev/zero of="$TESTFILE" bs=1M count="$count" oflag=direct conv=fdatasync 2>/dev/null ||
		dd if=/dev/zero of="$TESTFILE" bs=1M count="$count" conv=fdatasync 2>/dev/null
	t1=$(date +%s%N)
	w=$(awk -v c="$count" -v ns=$((t1 - t0)) 'BEGIN { printf "%.0f", c * 1024 / (ns / 1e9) }')

	status "disk: dd sequential read"
	t0=$(date +%s%N)
	dd if="$TESTFILE" of=/dev/null bs=1M iflag=direct 2>/dev/null ||
		dd if="$TESTFILE" of=/dev/null bs=1M 2>/dev/null
	t1=$(date +%s%N)
	r=$(awk -v c="$count" -v ns=$((t1 - t0)) 'BEGIN { printf "%.0f", c * 1024 / (ns / 1e9) }')

	clear_status
	DISK_ROWS=("1m $r $w $((r / 1024)) $((w / 1024))")
	print_disk "dd, sequential, $SIZE file"
}

print_disk() {
	[[ ${#DISK_ROWS[@]} -eq 0 ]] && { warn "disk test produced no results"; return; }
	DISK_DONE=1
	header "Disk" "$1"
	if [[ -n $DISK_FS || -n $DISK_MEDIA ]]; then
		kv "Device" "${DISK_MEDIA:-unknown}${DISK_FS:+, $DISK_FS}"
		say ""
	fi
	local row bs r w ri wi top=0
	for row in "${DISK_ROWS[@]}"; do
		read -r bs r w ri wi <<<"$row"
		top=$(awk -v a="$top" -v b="$(add "$r" "$w")" 'BEGIN { print (b > a) ? b : a }')
	done
	th '%-7s %-11s %-11s %-12s %-8s' "Block" "Read" "Write" "Total" "IOPS"
	for row in "${DISK_ROWS[@]}"; do
		read -r bs r w ri wi <<<"$row"
		printf '  %-7s %-11s %-11s %s%-12s%s %-8s %s\n' "$bs" "$(fmt_kbs "$r")" "$(fmt_kbs "$w")" \
			"$B" "$(fmt_kbs "$(add "$r" "$w")")" "$N" "$(fmt_iops "$(add "$ri" "$wi")")" \
			"$(bar "$(add "$r" "$w")" "$top" 14)" >&3
	done
}

# --- network ----------------------------------------------------------------

to_mbps() {
	awk -v v="$1" -v u="$2" 'BEGIN {
		if (u ~ /^G/) v *= 1000; else if (u ~ /^K/) v /= 1000; else if (u !~ /^M/) v /= 1e6
		printf "%.1f", v }'
}

# ping_ms <host> <proto> prints "avg_ms loss_pct"
ping_ms() {
	local out avg loss
	out=$(ping -"$2" -c 6 -i 0.3 -W 2 "$1" </dev/null 2>/dev/null)
	# old iputils has no -4, plain ping is v4 there
	[[ -z $out && $2 == 4 ]] && out=$(ping -c 6 -i 0.3 -W 2 "$1" </dev/null 2>/dev/null)
	# busybox ping has no -i
	[[ -z $out ]] && out=$(ping -c 4 -W 2 "$1" </dev/null 2>/dev/null)
	[[ -z $out ]] && return
	avg=$(sed -n 's/.*= [0-9.]*\/\([0-9.]*\)\/.*/\1/p' <<<"$out")
	loss=$(sed -n 's/.* \([0-9.]*\)% packet loss.*/\1/p' <<<"$out")
	echo "${avg:-0} ${loss:-0}"
}

# iperf_run <host> <ports> <proto> [-R], prints Mbit/s
iperf_run() {
	local host=$1 lo=${2%-*} hi=${2#*-} proto=$3 port out v
	local extra=("${@:4}")
	for _ in 1 2 3; do
		port=$((lo + RANDOM % (hi - lo + 1)))
		out=$(timeout $((IPERF_TIME + 15)) "$IPERF" -c "$host" -p "$port" -P 8 -t "$IPERF_TIME" \
			-"$proto" "${extra[@]}" </dev/null 2>&1)
		v=$(awk '/\[SUM\].*receiver/ { print $6, $7 }' <<<"$out")
		if [[ -n $v ]]; then
			to_mbps "${v% *}" "${v#* }"
			return 0
		fi
		sleep 1
	done
	return 1
}

net_test() {
	local protos=() p
	[[ -n $HAS_V4 && $PROTO != 6 ]] && protos+=(4)
	[[ -n $HAS_V6 && $PROTO != 4 ]] && protos+=(6)
	if [[ ${#protos[@]} -eq 0 ]]; then
		warn "no working ${PROTO:+IPv$PROTO }connectivity, skipping network test"
		return
	fi

	IPERF=$(command -v iperf3)
	if [[ -z $IPERF ]]; then
		status "net: fetching iperf3"
		IPERF=$(fetch_bin "$IPERF_TAG" iperf3)
	fi

	NET_ROWS=()
	for p in "${protos[@]}"; do
		if [[ -n $IPERF ]]; then
			net_iperf "$p"
		fi
	done
	if [[ ${#NET_ROWS[@]} -gt 0 ]]; then
		NET_TOOL=iperf3
	else
		[[ -n $IPERF ]] && warn "all iperf3 servers failed, trying http downloads instead"
		for p in "${protos[@]}"; do
			net_http "$p"
		done
		NET_TOOL="http download"
	fi
	clear_status
	print_net
}

# in_regions <region>: true when -l wasn't given or lists this region
in_regions() {
	local region=${1% x}
	[[ $1 == *" x" && -z $EXTENDED ]] && return 1
	[[ -z $REGIONS ]] && return 0
	[[ ",$REGIONS," == *",$region,"* ]]
}

net_iperf() {
	local proto=$1 entry host ports provider location region send recv ping loss n=0 total=0
	for entry in "${IPERF_SERVERS[@]}"; do
		IFS='|' read -r host ports provider location region <<<"$entry"
		in_regions "$region" && total=$((total + 1))
	done
	for entry in "${IPERF_SERVERS[@]}"; do
		IFS='|' read -r host ports provider location region <<<"$entry"
		in_regions "$region" || continue
		n=$((n + 1))
		status "net [$n/$total]: $location ($provider), IPv$proto, ping"
		read -r ping loss <<<"$(ping_ms "$host" "$proto")"
		status "net [$n/$total]: $location ($provider), IPv$proto, upload"
		send=$(iperf_run "$host" "$ports" "$proto")
		status "net [$n/$total]: $location ($provider), IPv$proto, download"
		recv=$(iperf_run "$host" "$ports" "$proto" -R)
		[[ -z $send && -z $recv ]] && continue
		NET_ROWS+=("$proto|$provider|$location|${send:-0}|${recv:-0}|${ping:-0}|${loss:-0}")
	done
}

net_http() {
	local proto=$1 entry url provider location host bps ping loss
	for entry in "${HTTP_SERVERS[@]}"; do
		IFS='|' read -r url provider location <<<"$entry"
		host=${url#*://}
		host=${host%%/*}
		status "net: $location ($provider), IPv$proto, download"
		read -r ping loss <<<"$(ping_ms "$host" "$proto")"
		bps=$(curl -s -"$proto" -o /dev/null --connect-timeout 5 --max-time "$IPERF_TIME" \
			-w '%{speed_download}' "$url" </dev/null)
		[[ -z $bps || $bps == 0 ]] && continue
		NET_ROWS+=("$proto|$provider|$location|0|$(awk -v b="$bps" 'BEGIN { printf "%.1f", b * 8 / 1e6 }')|${ping:-0}|${loss:-0}")
	done
}

print_net() {
	[[ ${#NET_ROWS[@]} -eq 0 ]] && { warn "network test produced no results"; return; }
	NET_DONE=1
	header "Network" "$NET_TOOL, upload and download"
	local row proto provider location send recv ping loss top=0 where
	for row in "${NET_ROWS[@]}"; do
		IFS='|' read -r proto provider location send recv ping loss <<<"$row"
		top=$(awk -v a="$top" -v b="$recv" 'BEGIN { print (b > a) ? b : a }')
	done
	th '%-34s %-12s %-12s %-8s %-6s' "Location" "Upload" "Download" "Ping" "Loss"
	for row in "${NET_ROWS[@]}"; do
		IFS='|' read -r proto provider location send recv ping loss <<<"$row"
		where="$location $D$provider$N"
		[[ $proto == 6 ]] && where+=" ${D}v6$N"
		local lost="$D-$N"
		awk -v l="$loss" 'BEGIN { exit !(l > 0) }' && lost="$Y$(awk -v l="$loss" 'BEGIN { printf "%.0f%%", l }')$N"
		printf '  %s %s %s %s %s %s\n' "$(pad 34 "$where")" "$(pad 12 "$(speed "$send")")" \
			"$(pad 12 "$B$(speed "$recv")$N")" "$(pad 8 "$(ping_color "$ping")")" "$(pad 6 "$lost")" "$(bar "$recv" "$top" 12)" >&3
	done
}

# --- summary ----------------------------------------------------------------

print_summary() {
	[[ -n $CPU_DONE || -n $DISK_DONE || -n $NET_DONE ]] || return
	header "Summary"
	if [[ -n $CPU_DONE ]]; then
		if (( THREADS > 1 )); then
			kv "CPU" "$B$(fmt_bytes "$CPU_SHA_N")$N sha256 on $THREADS threads, $(fmt_bytes "$CPU_SHA") on one"
		else
			kv "CPU" "$B$(fmt_bytes "$CPU_SHA")$N sha256 on one thread"
		fi
	fi
	if [[ -n $DISK_DONE ]]; then
		local row bs r w ri wi
		for row in "${DISK_ROWS[@]}"; do
			read -r bs r w ri wi <<<"$row"
			[[ $bs == 4k ]] && kv "Disk" "$B$(fmt_iops "$(add "$ri" "$wi")") IOPS$N at 4k, $(fmt_kbs "$(add "$r" "$w")")"
		done
	fi
	if [[ -n $NET_DONE ]]; then
		local proto provider location send recv ping loss best=0 bloc=
		for row in "${NET_ROWS[@]}"; do
			IFS='|' read -r proto provider location send recv ping loss <<<"$row"
			if awk -v a="$recv" -v b="$best" 'BEGIN { exit !(a > b) }'; then best=$recv; bloc=$location; fi
		done
		kv "Network" "$B$(fmt_mbps "$best")$N best download, from $bloc"
	fi
	kv "Time" "$(( (SECONDS - START) / 60 ))m $(( (SECONDS - START) % 60 ))s${QUICK:+, quick run}"
}

# --- result -----------------------------------------------------------------

build_json() {
	local first row
	printf '{"version":%s,"duration_s":%s' "$(jstr "$VERSION")" "$(jnum "$((SECONDS - START))")"
	[[ -n $QUICK ]] && printf ',"quick":true'
	printf ',"system":{"os":%s,"kernel":%s,"arch":%s,"cpu":%s,"cores":%s,"mhz":%s,"aes":%s,"vmx":%s,"virt":%s,"ram_kib":%s,"swap_kib":%s,"disk_kib":%s,"uptime_s":%s}' \
		"$(jstr "$SYS_OS")" "$(jstr "$SYS_KERNEL")" "$(jstr "$SYS_ARCH")" "$(jstr "$SYS_CPU")" \
		"$(jnum "$SYS_CORES")" "$(jnum "$SYS_MHZ")" "$(jbool "$SYS_AES")" "$(jbool "$SYS_VMX")" \
		"$(jstr "$SYS_VIRT")" "$(jnum "$SYS_RAM")" "$(jnum "$SYS_SWAP")" "$(jnum "$SYS_DISK")" "$(jnum "$SYS_UPTIME")"
	if [[ -n $LOC_ORG ]]; then
		printf ',"location":{"asn":%s,"org":%s,"country":%s}' \
			"$(jstr "$LOC_ASN")" "$(jstr "$LOC_ORG")" "$(jstr "$LOC_COUNTRY")"
	fi
	printf ',"ipv4":%s,"ipv6":%s' "$(jbool "$HAS_V4")" "$(jbool "$HAS_V6")"
	if [[ -n $BOARD ]]; then
		printf ',"leaderboard":true'
		[[ -n $BOARD_NAME ]] && printf ',"name":%s' "$(jstr "$BOARD_NAME")"
	fi

	if [[ -n $CPU_DONE ]]; then
		printf ',"cpu":{"openssl":%s,"threads":%s,"sha256_1":%s,"sha256_n":%s,"aes_1":%s,"aes_n":%s,"steal_pct":%s}' \
			"$(jstr "$CPU_OPENSSL")" "$(jnum "$THREADS")" "$(jnum "$CPU_SHA")" "$(jnum "$CPU_SHA_N")" \
			"$(jnum "$CPU_AES")" "$(jnum "$CPU_AES_N")" "$(jnum "${CPU_STEAL:-0}")"
	fi

	if [[ -n $DISK_DONE ]]; then
		printf ',"disk":{"tool":%s,"size":%s,"fs":%s,"media":%s,"tests":[' "$(jstr "$DISK_TOOL")" "$(jstr "$SIZE")" \
			"$(jstr "$DISK_FS")" "$(jstr "$DISK_MEDIA")"
		first=1
		local bs r w ri wi
		for row in "${DISK_ROWS[@]}"; do
			read -r bs r w ri wi <<<"$row"
			[[ -z $first ]] && printf ','
			first=
			printf '{"bs":%s,"read_kbs":%s,"write_kbs":%s,"read_iops":%s,"write_iops":%s}' \
				"$(jstr "$bs")" "$(jnum "$r")" "$(jnum "$w")" "$(jnum "$ri")" "$(jnum "$wi")"
		done
		printf ']}'
	fi

	if [[ -n $NET_DONE ]]; then
		printf ',"net":{"tool":%s,"tests":[' "$(jstr "$NET_TOOL")"
		first=1
		local proto provider location send recv ping loss
		for row in "${NET_ROWS[@]}"; do
			IFS='|' read -r proto provider location send recv ping loss <<<"$row"
			[[ -z $first ]] && printf ','
			first=
			printf '{"provider":%s,"location":%s,"proto":%s,"send_mbps":%s,"recv_mbps":%s,"ping_ms":%s,"loss_pct":%s}' \
				"$(jstr "$provider")" "$(jstr "$location")" "$proto" "$(jnum "$send")" "$(jnum "$recv")" "$(jnum "$ping")" "$(jnum "$loss")"
		done
		printf ']}'
	fi
	printf '}\n'
}

share() {
	local resp url
	status "uploading result"
	resp=$(curl -s --max-time 20 -X POST -H 'Content-Type: application/json' \
		--data-binary @- "$NODEBENCH_URL/api/results" <<<"$1")
	clear_status
	url=$(sed -n 's/.*"url":"\([^"]*\)".*/\1/p' <<<"$resp")
	if [[ -n $url && -n $JSON_ONLY ]]; then
		warn "Share: $url"
	elif [[ -n $url ]]; then
		say ""
		say "  $A$(rule 3)$N ${B}Your result$N  $A$url$N"
		[[ -n $BOARD ]] && say "  $D    listed on $NODEBENCH_URL/leaderboard once it passes the sanity checks$N"
	else
		local err
		err=$(sed -n 's/.*"error":"\([^"]*\)".*/\1/p' <<<"$resp")
		warn "upload failed${err:+: $err}"
	fi
}

cleanup() {
	clear_status 2>/dev/null
	[[ -n $TESTFILE ]] && rm -f "$TESTFILE"
	[[ -n $WORK ]] && rm -rf "$WORK"
}

main() {
	SKIP_CPU=; SKIP_DISK=; SKIP_NET=; PROTO=; SHARE=1; JSON_ONLY=; BOARD=; BOARD_NAME=; QUICK=; REGIONS=; EXTENDED=
	START=$SECONDS
	local size_set=
	DIR=$PWD
	SIZE=2G
	THREADS=$(nproc 2>/dev/null || echo 1)

	while [[ $# -gt 0 ]]; do
		case $1 in
			-c) SKIP_CPU=1 ;;
			-s) SKIP_DISK=1 ;;
			-n) SKIP_NET=1 ;;
			-4) PROTO=4 ;;
			-6) PROTO=6 ;;
			-d) DIR=$2; shift ;;
			-S) SIZE=$2; size_set=1; shift ;;
			-q) QUICK=1 ;;
			-x) EXTENDED=1 ;;
			-l) REGIONS=$(tr '[:upper:] ' '[:lower:],' <<<"$2"); shift ;;
			-t) THREADS=$2; shift ;;
			-L | --leaderboard)
				BOARD=1
				if [[ $# -gt 1 && $2 != -* ]]; then
					BOARD_NAME=$2
					shift
				fi
				;;
			--no-share) SHARE= ;;
			--json) JSON_ONLY=1 ;;
			-h | --help) usage; exit 0 ;;
			*) warn "unknown option: $1"; usage >&2; exit 1 ;;
		esac
		shift
	done
	if [[ -n $QUICK ]]; then
		FIO_TIME=5; IPERF_TIME=4; CPU_TIME=2
		[[ -z $size_set ]] && SIZE=512M
		if [[ -n $BOARD ]]; then
			warn "quick runs don't go on the leaderboard, ignoring -L"
			BOARD=
		fi
	fi
	if [[ -n $REGIONS ]]; then
		local r
		for r in ${REGIONS//,/ }; do
			[[ $r =~ ^(eu|na|sa|asia|oc)$ ]] || { warn "unknown region: $r (use eu, na, sa, asia, oc)"; exit 1; }
		done
	fi
	if [[ -n $BOARD && -z $SHARE ]]; then
		warn "-L needs the upload, ignoring --no-share"
		SHARE=1
	fi
	if (( ${#BOARD_NAME} > 32 )); then
		warn "leaderboard name is longer than 32 characters"
		exit 1
	fi
	if ! [[ $THREADS =~ ^[0-9]+$ ]] || (( THREADS < 1 )); then
		warn "bad thread count: $THREADS"
		exit 1
	fi

	if [[ -n $JSON_ONLY ]]; then
		exec 3>/dev/null
	else
		exec 3>&1
	fi

	if ! command -v curl >/dev/null; then
		warn "nodebench needs curl"
		exit 1
	fi

	WORK=$(mktemp -d "${TMPDIR:-/tmp}/nodebench.XXXXXX") || exit 1
	trap cleanup EXIT
	trap 'exit 130' INT TERM

	style_init
	say ""
	say "  $A$LOGO$N ${B}nodebench$N $D$VERSION$N"
	say "  ${D}cpu, disk and network benchmark  $SEP  ${NODEBENCH_URL#https://}  $SEP  $(date '+%Y-%m-%d %H:%M %Z')$N"

	status "collecting system info"
	collect_system
	clear_status
	print_system

	[[ -z $SKIP_CPU ]] && cpu_test
	[[ -z $SKIP_DISK ]] && disk_test
	[[ -z $SKIP_NET ]] && net_test
	print_summary

	local json
	json=$(build_json)
	[[ -n $JSON_ONLY ]] && printf '%s\n' "$json"

	if [[ -n $SHARE ]]; then
		if [[ -n $CPU_DONE || -n $DISK_DONE || -n $NET_DONE ]]; then
			share "$json"
		fi
	fi
	say ""
}

main "$@"
