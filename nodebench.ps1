# nodebench - cpu, disk and network benchmark, the Windows version
# https://github.com/instantnodeeu/nodebench
#
#   irm https://bench.instantnode.eu | iex
#   & ([scriptblock]::Create((irm https://bench.instantnode.eu))) -n --no-share
#
# Windows 10 or newer, Windows PowerShell 5.1 or PowerShell 7, no admin.
# Same options and the same result as nodebench.sh. The cpu test runs on the
# crypto built into Windows (CNG) instead of openssl. diskspd (Microsoft) and
# iperf3 (Windows build by ar51an) are fetched into a temp dir, checked against
# their sha256 and deleted again at the end.
#
# Everything runs inside this block, so iex leaves no variables or functions
# behind in your session. Kept to plain ascii on purpose, Windows PowerShell
# reads files without a BOM in the ansi codepage.

& {
$VERSION = '1.3.0'
$URL = if ($env:NODEBENCH_URL) { $env:NODEBENCH_URL } else { 'https://bench.instantnode.eu' }

$DISKSPD = @{
	Url = 'https://github.com/microsoft/diskspd/releases/download/v2.3/DiskSpd.ZIP'
	Sha = '18609981e8b04b4f645f1fd182ad0541615fe71722de36a9a933c54f747218a3'
}
$IPERF = @{
	Url = 'https://github.com/ar51an/iperf3-win-builds/releases/download/3.22/iperf-3.22-win64.zip'
	Sha = 'c9feabb3d721039508ccb81f74c2ada3ae11b9e75d33d24510573c2aa21da420'
}

# same list as nodebench.sh: host|port range|provider|location|region
$IPERF_SERVERS = @(
	'lg.instantnode.eu|5201|InstantNode|Eygelshoven, NL|eu'
	'lon.speedtest.clouvider.net|5200-5209|Clouvider|London, UK|eu'
	'iperf-ams-nl.eranium.net|5201-5210|Eranium|Amsterdam, NL|eu'
	'speedtest.fra1.de.leaseweb.net|5201-5210|Leaseweb|Frankfurt, DE|eu'
	'iperf3.moji.fr|5200-5240|Moji|Paris, FR|eu'
	'speedtest.wtnet.de|5200-5209|wilhelm.tel|Hamburg, DE|eu x'
	'speedtest.nyc1.us.leaseweb.net|5201-5210|Leaseweb|New York, US|na'
	'speedtest.chi11.us.leaseweb.net|5201-5210|Leaseweb|Chicago, US|na x'
	'speedtest.mia11.us.leaseweb.net|5201-5210|Leaseweb|Miami, US|na x'
	'speedtest.mtl2.ca.leaseweb.net|5201-5210|Leaseweb|Montreal, CA|na x'
	'speedtest.dal13.us.leaseweb.net|5201-5210|Leaseweb|Dallas, US|na'
	'la.speedtest.clouvider.net|5200-5209|Clouvider|Los Angeles, US|na'
	'speedtest.sao1.edgoo.net|9204-9240|Edgoo|Sao Paulo, BR|sa'
	'speedtest.sin1.sg.leaseweb.net|5201-5210|Leaseweb|Singapore, SG|asia'
	'speedtest.hkg12.hk.leaseweb.net|5201-5210|Leaseweb|Hong Kong, HK|asia x'
	'speedtest.tyo11.jp.leaseweb.net|5201-5210|Leaseweb|Tokyo, JP|asia'
	'speedtest.syd12.au.leaseweb.net|5201-5210|Leaseweb|Sydney, AU|oc'
)

$FIO_TIME = if ($env:NODEBENCH_FIO_TIME) { [int]$env:NODEBENCH_FIO_TIME } else { 20 }
$IPERF_TIME = if ($env:NODEBENCH_IPERF_TIME) { [int]$env:NODEBENCH_IPERF_TIME } else { 10 }
$CPU_TIME = if ($env:NODEBENCH_CPU_TIME) { [int]$env:NODEBENCH_CPU_TIME } else { 5 }

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
# older Windows 10 builds still default to TLS 1.0 in Windows PowerShell
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

# sha256 and aes-256-gcm through bcrypt.dll, the same code Windows uses for
# TLS and BitLocker. Plain P/Invoke so it compiles on Windows PowerShell's
# C# 5 compiler as well as on PowerShell 7.
$CNG = @'
using System;
using System.Diagnostics;
using System.Runtime.InteropServices;
using System.Text;
using System.Threading;

public static class NodebenchCng
{
	[StructLayout(LayoutKind.Sequential)]
	struct AuthInfo
	{
		public int Size, Version;
		public IntPtr Nonce; public int NonceLen;
		public IntPtr AuthData; public int AuthDataLen;
		public IntPtr Tag; public int TagLen;
		public IntPtr MacContext; public int MacContextLen;
		public int AadLen; public long DataLen; public int Flags;
	}

	[DllImport("bcrypt.dll", CharSet = CharSet.Unicode)]
	static extern int BCryptOpenAlgorithmProvider(out IntPtr alg, string id, string impl, int flags);
	[DllImport("bcrypt.dll")]
	static extern int BCryptCloseAlgorithmProvider(IntPtr alg, int flags);
	[DllImport("bcrypt.dll", CharSet = CharSet.Unicode)]
	static extern int BCryptSetProperty(IntPtr obj, string prop, byte[] value, int len, int flags);
	[DllImport("bcrypt.dll")]
	static extern int BCryptGenerateSymmetricKey(IntPtr alg, out IntPtr key, IntPtr obj, int objLen, byte[] secret, int secretLen, int flags);
	[DllImport("bcrypt.dll")]
	static extern int BCryptDestroyKey(IntPtr key);
	[DllImport("bcrypt.dll")]
	static extern int BCryptHash(IntPtr alg, byte[] secret, int secretLen, byte[] input, int inputLen, byte[] output, int outputLen);
	[DllImport("bcrypt.dll")]
	static extern int BCryptEncrypt(IntPtr key, byte[] input, int inputLen, ref AuthInfo info, IntPtr iv, int ivLen, byte[] output, int outputLen, out int written, int flags);

	const int Block = 16384;

	static void Check(int status)
	{
		if (status != 0) throw new Exception("bcrypt error 0x" + status.ToString("x8"));
	}

	// Speed returns bytes per second summed over all threads, like
	// openssl speed -multi does.
	public static double Speed(bool aes, int threads, double seconds)
	{
		double[] rates = new double[threads];
		Exception error = null;
		Thread[] ts = new Thread[threads];
		for (int i = 0; i < threads; i++)
		{
			int n = i;
			ts[i] = new Thread(delegate ()
			{
				try { rates[n] = Run(aes, seconds); }
				catch (Exception e) { error = e; }
			});
			ts[i].Start();
		}
		double sum = 0;
		for (int i = 0; i < threads; i++)
		{
			ts[i].Join();
			sum += rates[i];
		}
		if (error != null) throw error;
		return sum;
	}

	static double Run(bool aes, double seconds)
	{
		byte[] input = new byte[Block], output = new byte[aes ? Block : 32];
		IntPtr alg, key = IntPtr.Zero;
		AuthInfo info = new AuthInfo();
		Check(BCryptOpenAlgorithmProvider(out alg, aes ? "AES" : "SHA256", null, 0));
		try
		{
			if (aes)
			{
				byte[] mode = Encoding.Unicode.GetBytes("ChainingModeGCM\0");
				Check(BCryptSetProperty(alg, "ChainingMode", mode, mode.Length, 0));
				Check(BCryptGenerateSymmetricKey(alg, out key, IntPtr.Zero, 0, new byte[32], 32, 0));
				info.Size = Marshal.SizeOf(typeof(AuthInfo));
				info.Version = 1;
				info.Nonce = Marshal.AllocHGlobal(12);
				info.NonceLen = 12;
				info.Tag = Marshal.AllocHGlobal(16);
				info.TagLen = 16;
			}
			long blocks = 0, limit = (long)(seconds * Stopwatch.Frequency);
			int written;
			Stopwatch sw = Stopwatch.StartNew();
			while (sw.ElapsedTicks < limit)
			{
				for (int i = 0; i < 16; i++)
				{
					if (aes) Check(BCryptEncrypt(key, input, Block, ref info, IntPtr.Zero, 0, output, Block, out written, 0));
					else Check(BCryptHash(alg, null, 0, input, Block, output, 32));
				}
				blocks += 16;
			}
			return blocks * (double)Block / sw.Elapsed.TotalSeconds;
		}
		finally
		{
			if (key != IntPtr.Zero) BCryptDestroyKey(key);
			if (info.Nonce != IntPtr.Zero) Marshal.FreeHGlobal(info.Nonce);
			if (info.Tag != IntPtr.Zero) Marshal.FreeHGlobal(info.Tag);
			BCryptCloseAlgorithmProvider(alg, 0);
		}
	}
}
'@

# --- output -----------------------------------------------------------------

# PowerShell variables ignore case, so the colors get names no local uses
$Bo = $Di = $Ac = $Gr = $Ye = $Re = $No = ''
$ESC = [char]27
$VT = $Host.UI.SupportsVirtualTerminal -and -not $env:NO_COLOR
if ($VT) {
	$Bo = "$ESC[1m"; $Di = "$ESC[2m"; $No = "$ESC[0m"
	$Ac = "$ESC[38;2;59;130;246m"; $Gr = "$ESC[38;2;63;185;80m"; $Ye = "$ESC[38;2;227;179;65m"; $Re = "$ESC[38;2;248;81;73m"
}
function chars([int[]]$c) { -join ($c | ForEach-Object { [char]$_ }) }
# the old console host has no braille or block glyphs, Windows Terminal does
if ($env:WT_SESSION) {
	$HL = chars 0x2500; $BLK = chars 0x2501; $EMP = chars 0x2500; $SEP = chars 0x2502
	$LOGO = chars 0x2581, 0x2583, 0x2585, 0x2587
	$SPIN = chars 0x280B, 0x2819, 0x2839, 0x2838, 0x283C, 0x2834, 0x2826, 0x2827, 0x2807, 0x280F
} else {
	$HL = '-'; $BLK = '#'; $EMP = '.'; $SEP = '|'; $LOGO = '[nb]'; $SPIN = '|/-\'
}
$INV = [Globalization.CultureInfo]::InvariantCulture
$ui = @{ Step = 0; Quiet = $false }

function say([string]$text) { if (-not $ui.Quiet) { Write-Host $text } }
function warn([string]$text) { Write-Host "  $Ye!$No $text" }

function status([string]$text) {
	if (-not $VT -or $ui.Quiet) { return }
	$c = $SPIN[$ui.Step % $SPIN.Length]
	$ui.Step++
	Write-Host -NoNewline "`r$ESC[K  $Ac$c$No $Di$text$No"
}
function clear_status { if ($VT -and -not $ui.Quiet) { Write-Host -NoNewline "`r$ESC[K" } }

function rule([int]$n = 66) { $HL * $n }
function header([string]$title, [string]$detail) {
	say ''
	if ($detail) { say "  $Bo$title$No  $Di$detail$No" } else { say "  $Bo$title$No" }
	say "  $Di$(rule)$No"
}
function kv([string]$label, [string]$value) { say ("  $Di" + $label.PadRight(12) + "$No $value") }
function yesno($v) { if ($v) { "${Gr}yes$No" } else { "${Di}no$No" } }

function bar([double]$v, [double]$top, [int]$w) {
	$n = 0
	if ($top -gt 0) { $n = [int][math]::Floor($v / $top * $w + 0.5) }
	if ($v -gt 0 -and $n -lt 1) { $n = 1 }
	if ($n -gt $w) { $n = $w }
	"$Ac$($BLK * $n)$Di$($EMP * ($w - $n))$No"
}
# pad: left align text that may contain color codes
function pad([int]$w, [string]$text) {
	$plain = $text -replace "$ESC\[[0-9;]*m", ''
	$text + (' ' * [math]::Max(0, $w - $plain.Length))
}

function fx([string]$f, $v) { [string]::Format($INV, $f, $v) }
function fmt_bytes([double]$v) { if ($v -ge 1e9) { fx '{0:0.00} GB/s' ($v / 1e9) } else { fx '{0:0} MB/s' ($v / 1e6) } }
function fmt_kbs([double]$v) {
	if ($v -le 0) { '-' }
	elseif ($v -ge 1048576) { fx '{0:0.00} GB/s' ($v / 1048576) }
	elseif ($v -ge 1024) { fx '{0:0.0} MB/s' ($v / 1024) }
	else { fx '{0:0} KB/s' $v }
}
function fmt_iops([double]$v) {
	if ($v -ge 1e6) { fx '{0:0.00}M' ($v / 1e6) }
	elseif ($v -ge 1e5) { fx '{0:0}k' ($v / 1e3) }
	elseif ($v -ge 1e4) { fx '{0:0.0}k' ($v / 1e3) }
	else { fx '{0:0}' $v }
}
function fmt_mbps([double]$v) { if ($v -le 0) { '-' } elseif ($v -ge 1000) { fx '{0:0.00} Gbit/s' ($v / 1000) } else { fx '{0:0} Mbit/s' $v } }
function fmt_ms([double]$v) { if ($v -le 0) { '-' } elseif ($v -lt 10) { fx '{0:0.0} ms' $v } else { fx '{0:0} ms' $v } }
function fmt_kib([double]$v) {
	$units = 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'
	$i = 0
	while ($v -ge 1024 -and $i -lt 4) { $v /= 1024; $i++ }
	fx ('{0:0.0} ' + $units[$i]) $v
}
function ping_color([double]$ms) {
	$v = fmt_ms $ms
	if ($v -eq '-') { "$Di-$No" } elseif ($ms -lt 50) { "$Gr$v$No" } elseif ($ms -lt 150) { "$Ye$v$No" } else { "$Re$v$No" }
}
function speed([double]$mbps) { if ($mbps -le 0) { "${Re}failed$No" } else { fmt_mbps $mbps } }

function usage {
	Write-Host @"
nodebench $VERSION, Windows version

usage: & ([scriptblock]::Create((irm $URL))) [options]

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
"@
}

# --- helpers for external tools ---------------------------------------------

# run <exe> <arguments> <timeout s>: stdout, or '' after a timeout. stderr is
# drained so the tool can't block on it, diskspd puts warnings there.
function run([string]$exe, [string]$arguments, [int]$timeout) {
	$p = New-Object Diagnostics.Process
	$p.StartInfo.FileName = $exe
	$p.StartInfo.Arguments = $arguments
	$p.StartInfo.UseShellExecute = $false
	$p.StartInfo.RedirectStandardOutput = $true
	$p.StartInfo.RedirectStandardError = $true
	$p.StartInfo.CreateNoWindow = $true
	try { [void]$p.Start() } catch { return '' }
	$tmp.Proc = $p
	$out = $p.StandardOutput.ReadToEndAsync()
	$err = $p.StandardError.ReadToEndAsync()
	if (-not $p.WaitForExit($timeout * 1000)) {
		try { $p.Kill() } catch {}
		return ''
	}
	$tmp.Proc = $null
	[void]$err.Result
	$out.Result
}

# fetch_tool <tool> <exe name>: downloads and unpacks the zip into the work
# dir, returns the path of the exe for this cpu
function fetch_tool($tool, [string]$name) {
	$zip = Join-Path $tmp.Work ([IO.Path]::GetFileName($tool.Url))
	try {
		Invoke-WebRequest -Uri $tool.Url -OutFile $zip -UseBasicParsing
	} catch {
		return
	}
	if ((Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash -ne $tool.Sha) {
		warn "checksum mismatch for $name, not using it"
		return
	}
	$dir = Join-Path $tmp.Work ([IO.Path]::GetFileNameWithoutExtension($zip))
	Expand-Archive -LiteralPath $zip -DestinationPath $dir
	$arch = if ($sys.arch -eq 'arm64') { 'arm64' } else { 'amd64' }
	$all = @(Get-ChildItem -LiteralPath $dir -Recurse -Filter $name)
	$exe = @($all | Where-Object { $_.FullName -like "*\$arch\*" }) + $all | Select-Object -First 1
	if ($exe) { $exe.FullName }
}

# --- system info ------------------------------------------------------------

function virt_type($cs) {
	switch -Regex ("$($cs.Manufacturer) $($cs.Model)".ToLower()) {
		'vmware' { return 'vmware' }
		'virtualbox|innotek' { return 'oracle' }
		'qemu|kvm|bochs' { return 'kvm' }
		'xen' { return 'xen' }
		'microsoft corporation virtual machine' { return 'microsoft' }
		'amazon ec2' { return 'amazon' }
		'google compute' { return 'google' }
		'parallels' { return 'parallels' }
	}
	'none'
}

# reachable <ip> <family>: a tcp connect to cloudflare on 443
function reachable([string]$ip, $family) {
	$c = New-Object Net.Sockets.TcpClient($family)
	try { $c.ConnectAsync($ip, 443).Wait(5000) } catch { $false } finally { $c.Close() }
}

function collect_system {
	$os = Get-CimInstance Win32_OperatingSystem
	$cs = Get-CimInstance Win32_ComputerSystem
	$cpu = @(Get-CimInstance Win32_Processor)[0]

	$release = ''
	try { $release = (Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion').DisplayVersion } catch {}
	$sys.os = ("$($os.Caption -replace '^Microsoft\s+', '') $release").Trim()
	$sys.kernel = "NT $($os.Version)"
	$a = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
	$sys.arch = switch ($a) { 'AMD64' { 'x86_64' } 'ARM64' { 'arm64' } 'x86' { 'i686' } default { "$a".ToLower() } }
	$sys.cpu = ($cpu.Name -replace '\s+', ' ').Trim()
	if (-not $sys.cpu) { $sys.cpu = 'unknown' }
	$sys.cores = [Environment]::ProcessorCount
	$sys.mhz = [int]$cpu.MaxClockSpeed
	$sys.virt = virt_type $cs
	# with Hyper-V or VBS on, Windows itself runs under the hypervisor and the
	# firmware flag reads false, the cpu obviously has VT-x then
	$sys.vmx = [bool]$cpu.VirtualizationFirmwareEnabled -or ([bool]$cs.HypervisorPresent -and $sys.virt -eq 'none')
	# .NET Framework can't ask the cpu, PowerShell 7 can. Otherwise the gcm
	# speed decides after the cpu test.
	$sys.aes = $null
	try { $sys.aes = [System.Runtime.Intrinsics.X86.Aes]::IsSupported -or [System.Runtime.Intrinsics.Arm.Aes]::IsSupported } catch {}

	$sys.ram = [long][math]::Floor($cs.TotalPhysicalMemory / 1KB)
	$sys.swap = [long](@(Get-CimInstance Win32_PageFileUsage) | Measure-Object -Property AllocatedBaseSize -Sum).Sum * 1024
	$sys.disk = [long][math]::Floor([double](@(Get-CimInstance Win32_LogicalDisk -Filter 'DriveType=3') | Measure-Object -Property Size -Sum).Sum / 1KB)
	$sys.uptime = [long]((Get-Date) - $os.LastBootUpTime).TotalSeconds

	$sys.v4 = reachable '1.1.1.1' ([Net.Sockets.AddressFamily]::InterNetwork)
	$sys.v6 = reachable '2606:4700:4700::1111' ([Net.Sockets.AddressFamily]::InterNetworkV6)

	try {
		$info = Invoke-RestMethod -Uri 'https://ipinfo.io/json' -TimeoutSec 5
		if ("$($info.org)" -match '^(AS\d+) (.*)$') {
			$loc.asn = $Matches[1]
			$loc.org = $Matches[2]
		} else {
			$loc.org = "$($info.org)"
		}
		$loc.country = "$($info.country)"
	} catch {}
	# the share server may know a better name for this network, ask with the asn only
	if ($opt.Share -and $loc.asn) {
		try {
			$alias = Invoke-RestMethod -Uri "$URL/api/alias?asn=$($loc.asn)" -TimeoutSec 3
			if ($alias.org) {
				$loc.org = $alias.org
				if ($alias.country) { $loc.country = $alias.country }
			}
		} catch {}
	}
}

function print_system {
	$up = [TimeSpan]::FromSeconds($sys.uptime)
	$on = "${Gr}online$No"; $off = "${Di}offline$No"
	header 'System'
	kv 'Processor' "$Bo$($sys.cpu)$No"
	$cores = "$($sys.cores)"
	if ($sys.mhz) { $cores += " @ $($sys.mhz) MHz" }
	kv 'Cores' $cores
	if ($null -ne $sys.aes) { kv 'AES-NI' (yesno $sys.aes) }
	kv 'VM-x/AMD-V' (yesno $sys.vmx)
	kv 'Memory' "$(fmt_kib $sys.ram) RAM, $(fmt_kib $sys.swap) swap"
	kv 'Disk' (fmt_kib $sys.disk)
	kv 'OS' $sys.os
	kv 'Kernel' "$($sys.kernel) ($($sys.arch))"
	kv 'Virt' $sys.virt
	kv 'Uptime' "$($up.Days)d $($up.Hours)h $($up.Minutes)m"
	kv 'Network' ("IPv4 " + $(if ($sys.v4) { $on } else { $off }) + ", IPv6 " + $(if ($sys.v6) { $on } else { $off }))
	if ($loc.org) {
		$p = $loc.org
		if ($loc.asn) { $p = "$($loc.asn) $p" }
		if ($loc.country) { $p += " ($($loc.country))" }
		kv 'Provider' $p
	}
}

# --- cpu --------------------------------------------------------------------

function cpu_test {
	$t = $opt.Threads
	try {
		if (-not ('NodebenchCng' -as [type])) { Add-Type -TypeDefinition $CNG }
		status 'cpu: sha256, 1 thread'
		$s1 = [NodebenchCng]::Speed($false, 1, $CPU_TIME)
		status 'cpu: aes-256-gcm, 1 thread'
		$a1 = [NodebenchCng]::Speed($true, 1, $CPU_TIME)
		$sn = $s1; $an = $a1
		if ($t -gt 1) {
			status "cpu: sha256, $t threads"
			$sn = [NodebenchCng]::Speed($false, $t, $CPU_TIME)
			status "cpu: aes-256-gcm, $t threads"
			$an = [NodebenchCng]::Speed($true, $t, $CPU_TIME)
		}
	} catch {
		clear_status
		warn "cpu test failed: $($_.Exception.Message)"
		return
	}
	clear_status
	$res.cpu = [ordered]@{ openssl = 'Windows CNG'; threads = [int]$t; sha256_1 = $s1; sha256_n = $sn; aes_1 = $a1; aes_n = $an }
	# software aes-gcm stays well under 200 MB/s, with AES-NI or the arm
	# crypto extensions it's several GB/s
	if ($null -eq $sys.aes) { $sys.aes = $a1 -gt 5e8 }

	header 'CPU' 'Windows CNG, 16 KiB blocks'
	if ($t -gt 1) {
		say ("  $Di" + 'Test'.PadRight(14) + ' ' + '1 thread'.PadRight(12) + ' ' + "$t threads".PadRight(12) + $No)
	} else {
		say ("  $Di" + 'Test'.PadRight(14) + ' ' + '1 thread'.PadRight(12) + $No)
	}
	foreach ($row in @(@('sha256', $s1, $sn), @('aes-256-gcm', $a1, $an))) {
		if ($t -gt 1) {
			say ("  " + $row[0].PadRight(14) + ' ' + (fmt_bytes $row[1]).PadRight(12) + " $Bo" + (fmt_bytes $row[2]).PadRight(12) + "$No " + (bar $row[1] $row[2] 24))
		} else {
			say ("  " + $row[0].PadRight(14) + " $Bo" + (fmt_bytes $row[1]).PadRight(12) + $No)
		}
	}
	if ($t -gt 1 -and $s1 -gt 0) {
		say ''
		kv 'Scaling' ("$Ac" + (fx '{0:0.0}x' ($sn / $s1)) + "$No from 1 to $t threads")
	}
}

# --- disk -------------------------------------------------------------------

function size_bytes([string]$s) {
	if ($s -match '^(\d+)([KMG])$') { [long]$Matches[1] * @{ K = 1KB; M = 1MB; G = 1GB }[$Matches[2]] }
}

function xml_sum([string]$xml, [string]$tag) {
	$sum = 0.0
	foreach ($m in [regex]::Matches($xml, "<$tag>([\d.]+)</$tag>")) { $sum += [double]$m.Groups[1].Value }
	$sum
}

function disk_media($drive) {
	$res.diskfs = "$($drive.DriveFormat)"
	$res.media = ''
	if ($sys.virt -ne 'none') {
		$res.media = 'virtual'
		return
	}
	try {
		$disk = Get-Partition -DriveLetter $drive.Name[0] | Get-Disk
		$pd = Get-PhysicalDisk | Where-Object { "$($_.DeviceId)" -eq "$($disk.Number)" } | Select-Object -First 1
		# the storage module hands out names or the raw enum numbers, depending on the version
		if ("$($disk.BusType)" -in 'NVMe', '17') { $res.media = 'nvme' }
		elseif ("$($pd.MediaType)" -in 'SSD', '4') { $res.media = 'ssd' }
		elseif ("$($pd.MediaType)" -in 'HDD', '3') { $res.media = 'hdd' }
	} catch {}
}

function disk_test {
	$bytes = size_bytes $opt.Size
	if (-not $bytes) {
		warn "bad test file size: $($opt.Size)"
		return
	}
	if (-not (Test-Path -LiteralPath $opt.Dir -PathType Container)) {
		warn "can't write to $($opt.Dir), skipping disk test"
		return
	}
	$dir = (Resolve-Path -LiteralPath $opt.Dir).ProviderPath
	try {
		$drive = New-Object IO.DriveInfo([IO.Path]::GetPathRoot($dir))
		$free = $drive.AvailableFreeSpace
	} catch {
		warn "$dir is not on a local drive, skipping disk test"
		return
	}
	if ($free -lt $bytes * 1.2) {
		warn "not enough free space in $dir for a $($opt.Size) test file, skipping disk test"
		return
	}
	disk_media $drive

	status 'disk: fetching diskspd'
	$exe = fetch_tool $DISKSPD 'diskspd.exe'
	if (-not $exe) {
		clear_status
		warn 'diskspd not available, skipping disk test'
		return
	}

	# lay the file out once so the timed runs don't include it
	status "disk: writing $($opt.Size) test file"
	$tmp.File = Join-Path $dir "nodebench.$PID.dat"
	try {
		$buf = New-Object byte[] 1MB
		(New-Object Random).NextBytes($buf)
		$fs = [IO.File]::Create($tmp.File, 1MB, [IO.FileOptions]::WriteThrough)
		try {
			for ($done = 0L; $done -lt $bytes; $done += $buf.Length) { $fs.Write($buf, 0, $buf.Length) }
		} finally {
			$fs.Close()
		}
	} catch {
		clear_status
		warn "can't write the test file to ${dir}: $($_.Exception.Message)"
		return
	}

	# same as fio in nodebench.sh: 2 threads, 64 in flight each, 50/50 random
	# read/write, no os cache (-Su is FILE_FLAG_NO_BUFFERING, like O_DIRECT)
	$rows = @()
	foreach ($bs in '4k', '64k', '512k', '1m') {
		status "disk: random read/write, $bs blocks"
		$out = run $exe ("-b$($bs.ToUpper()) -d$FIO_TIME -W0 -C0 -t2 -o64 -r -w50 -Su -Rxml `"$($tmp.File)`"") ($FIO_TIME + 60)
		# the counters are summed over the threads. Not parsed as xml: diskspd
		# writes Name="<unknown>" when it can't read the power scheme.
		$secs = xml_sum $out 'TestTimeSeconds'
		if ($secs -gt 0) {
			$rows += , [ordered]@{
				bs = $bs; read_kbs = (xml_sum $out 'ReadBytes') / 1KB / $secs; write_kbs = (xml_sum $out 'WriteBytes') / 1KB / $secs
				read_iops = (xml_sum $out 'ReadCount') / $secs; write_iops = (xml_sum $out 'WriteCount') / $secs
			}
		}
	}
	Remove-Item -LiteralPath $tmp.File -Force -ErrorAction SilentlyContinue
	clear_status
	if (-not $rows) {
		warn 'disk test produced no results'
		return
	}
	$res.disk = [ordered]@{ tool = 'diskspd'; size = $opt.Size; fs = $res.diskfs; media = $res.media; tests = $rows }

	header 'Disk' "diskspd, random 50/50 read/write, $($opt.Size) file"
	if ($res.diskfs -or $res.media) {
		$dev = if ($res.media) { $res.media } else { 'unknown' }
		if ($res.diskfs) { $dev += ", $($res.diskfs.ToLower())" }
		kv 'Device' $dev
		say ''
	}
	$top = ($rows | ForEach-Object { $_.read_kbs + $_.write_kbs } | Measure-Object -Maximum).Maximum
	say ("  $Di" + 'Block'.PadRight(7) + ' ' + 'Read'.PadRight(11) + ' ' + 'Write'.PadRight(11) + ' ' + 'Total'.PadRight(12) + ' ' + 'IOPS'.PadRight(8) + $No)
	foreach ($r in $rows) {
		$sum = $r.read_kbs + $r.write_kbs
		say ("  " + $r.bs.PadRight(7) + ' ' + (fmt_kbs $r.read_kbs).PadRight(11) + ' ' + (fmt_kbs $r.write_kbs).PadRight(11) +
			" $Bo" + (fmt_kbs $sum).PadRight(12) + "$No " + (fmt_iops ($r.read_iops + $r.write_iops)).PadRight(8) + ' ' + (bar $sum $top 14))
	}
}

# --- network ----------------------------------------------------------------

# ping_ms <host> <proto>: average ms and loss percent, 0 0 when it can't ping
function ping_ms([string]$name, [int]$proto) {
	try {
		$family = if ($proto -eq 6) { 'InterNetworkV6' } else { 'InterNetwork' }
		$ip = [Net.Dns]::GetHostAddresses($name) | Where-Object { "$($_.AddressFamily)" -eq $family } | Select-Object -First 1
		if (-not $ip) { return 0, 0 }
		$p = New-Object Net.NetworkInformation.Ping
		$times = @()
		for ($i = 0; $i -lt 6; $i++) {
			$r = $p.Send($ip, 2000)
			if ("$($r.Status)" -eq 'Success') { $times += $r.RoundtripTime }
			Start-Sleep -Milliseconds 300
		}
		$avg = 0
		if ($times) { $avg = ($times | Measure-Object -Average).Average }
		return $avg, ((6 - $times.Count) / 6 * 100)
	} catch {
		return 0, 0
	}
}

# iperf_run <exe> <host> <ports> <proto> [-R]: Mbit/s, or nothing
function iperf_run([string]$exe, [string]$name, [string]$ports, [int]$proto, [string]$extra = '') {
	$range = $ports -split '-'
	$lo = [int]$range[0]
	$hi = [int]$range[-1]
	for ($try = 0; $try -lt 3; $try++) {
		$port = Get-Random -Minimum $lo -Maximum ($hi + 1)
		$out = run $exe "-c $name -p $port -P 8 -t $IPERF_TIME -$proto $extra" ($IPERF_TIME + 15)
		foreach ($line in $out -split "`n") {
			if ($line -match '\[SUM\].*?([\d.]+) ([KMG]?)bits/sec.*receiver') {
				$v = [double]$Matches[1]
				switch ($Matches[2]) { 'G' { $v *= 1000 } 'K' { $v /= 1000 } '' { $v /= 1e6 } }
				return $v
			}
		}
		Start-Sleep -Seconds 1
	}
}

function in_regions([string]$region) {
	if ($region.EndsWith(' x') -and -not $opt.Extended) { return $false }
	if (-not $opt.Regions) { return $true }
	",$($opt.Regions)," -like "*,$($region -replace ' x$', ''),*"
}

function net_test {
	$protos = @()
	if ($sys.v4 -and $opt.Proto -ne '6') { $protos += 4 }
	if ($sys.v6 -and $opt.Proto -ne '4') { $protos += 6 }
	if (-not $protos) {
		$which = if ($opt.Proto) { "IPv$($opt.Proto) " } else { '' }
		warn "no working ${which}connectivity, skipping network test"
		return
	}
	status 'net: fetching iperf3'
	$exe = fetch_tool $IPERF 'iperf3.exe'
	if (-not $exe) {
		clear_status
		warn 'iperf3 not available, skipping network test'
		return
	}

	$list = @($IPERF_SERVERS | Where-Object { in_regions (($_ -split '\|')[4]) })
	$rows = @()
	foreach ($proto in $protos) {
		$n = 0
		foreach ($entry in $list) {
			$name, $ports, $provider, $location, $region = $entry -split '\|'
			$n++
			$at = "net $n/$($list.Count) $location, IPv$proto"
			status "$at, ping"
			$ping, $loss = ping_ms $name $proto
			status "$at, upload"
			$send = iperf_run $exe $name $ports $proto
			status "$at, download"
			$recv = iperf_run $exe $name $ports $proto '-R'
			if ($null -eq $send -and $null -eq $recv) { continue }
			$rows += , [ordered]@{ provider = $provider; location = $location; proto = $proto; send_mbps = [double]$send; recv_mbps = [double]$recv; ping_ms = [double]$ping; loss_pct = [double]$loss }
		}
	}
	clear_status
	if (-not $rows) {
		warn 'network test produced no results'
		return
	}
	$res.net = [ordered]@{ tool = 'iperf3'; tests = $rows }

	header 'Network' 'iperf3, upload and download'
	$top = ($rows | ForEach-Object { $_.recv_mbps } | Measure-Object -Maximum).Maximum
	say ("  $Di" + 'Location'.PadRight(34) + ' ' + 'Upload'.PadRight(12) + ' ' + 'Download'.PadRight(12) + ' ' + 'Ping'.PadRight(8) + ' ' + 'Loss'.PadRight(6) + $No)
	foreach ($r in $rows) {
		$where = "$($r.location) $Di$($r.provider)$No"
		if ($r.proto -eq 6) { $where += " ${Di}v6$No" }
		$lost = "$Di-$No"
		if ($r.loss_pct -gt 0) { $lost = "$Ye" + (fx '{0:0}%' $r.loss_pct) + $No }
		say ("  " + (pad 34 $where) + ' ' + (pad 12 (speed $r.send_mbps)) + ' ' + (pad 12 "$Bo$(speed $r.recv_mbps)$No") + ' ' +
			(pad 8 (ping_color $r.ping_ms)) + ' ' + (pad 6 $lost) + ' ' + (bar $r.recv_mbps $top 12))
	}
}

# --- summary and result -----------------------------------------------------

function print_summary {
	if (-not ($res.cpu -or $res.disk -or $res.net)) { return }
	header 'Summary'
	if ($c = $res.cpu) {
		if ($c.threads -gt 1) {
			kv 'CPU' "$Bo$(fmt_bytes $c.sha256_n)$No sha256 on $($c.threads) threads, $(fmt_bytes $c.sha256_1) on one"
		} else {
			kv 'CPU' "$Bo$(fmt_bytes $c.sha256_1)$No sha256 on one thread"
		}
	}
	if ($d = $res.disk) {
		$r = @($d.tests | Where-Object { $_.bs -eq '4k' }) + @($d.tests) | Select-Object -First 1
		kv 'Disk' "$Bo$(fmt_iops ($r.read_iops + $r.write_iops)) IOPS$No at $($r.bs), $(fmt_kbs ($r.read_kbs + $r.write_kbs))"
	}
	if ($n = $res.net) {
		$best = $n.tests | Sort-Object { $_.recv_mbps } -Descending | Select-Object -First 1
		if ($best.recv_mbps -gt 0) {
			kv 'Network' "$Bo$(fmt_mbps $best.recv_mbps)$No best download, from $($best.location)"
		} else {
			kv 'Network' "${Re}every download failed$No"
		}
	}
	$took = $clock.Elapsed
	$quick = if ($opt.Quick) { ', quick run' } else { '' }
	kv 'Time' ("{0}m {1}s$quick" -f [int][math]::Floor($took.TotalMinutes), $took.Seconds)
}

function build_json {
	$j = [ordered]@{ version = $VERSION; duration_s = [long]$clock.Elapsed.TotalSeconds }
	if ($opt.Quick) { $j.quick = $true }
	$j.system = [ordered]@{
		os = $sys.os; kernel = $sys.kernel; arch = $sys.arch; cpu = $sys.cpu; cores = [int]$sys.cores; mhz = [int]$sys.mhz
		aes = [bool]$sys.aes; vmx = [bool]$sys.vmx; virt = $sys.virt; ram_kib = $sys.ram; swap_kib = $sys.swap; disk_kib = $sys.disk; uptime_s = $sys.uptime
	}
	if ($loc.org) { $j.location = [ordered]@{ asn = "$($loc.asn)"; org = $loc.org; country = "$($loc.country)" } }
	$j.ipv4 = [bool]$sys.v4
	$j.ipv6 = [bool]$sys.v6
	if ($opt.Board) {
		$j.leaderboard = $true
		if ($opt.Name) { $j.name = $opt.Name }
	}
	foreach ($k in 'cpu', 'disk', 'net') { if ($res[$k]) { $j[$k] = $res[$k] } }
	$j | ConvertTo-Json -Depth 6 -Compress
}

function share([string]$json) {
	status 'uploading result'
	try {
		$r = Invoke-RestMethod -Method Post -Uri "$URL/api/results" -ContentType 'application/json' -Body ([Text.Encoding]::UTF8.GetBytes($json)) -TimeoutSec 20
		clear_status
		if ($opt.Json) {
			warn "Share: $($r.url)"
		} else {
			say ''
			say "  $Ac$(rule 3)$No ${Bo}Your result$No  $Ac$($r.url)$No"
			if ($opt.Board) { say "  $Di    listed on $URL/leaderboard once it passes the sanity checks$No" }
		}
	} catch {
		clear_status
		$msg = ''
		try { $msg = ($_.ErrorDetails.Message | ConvertFrom-Json).error } catch {}
		if ($msg) { warn "upload failed: $msg" } else { warn 'upload failed' }
	}
}

# --- main -------------------------------------------------------------------

if ($env:OS -ne 'Windows_NT') {
	warn "this is the Windows version, on Linux and macOS run: curl -sL $URL | bash"
	return
}

$opt = @{
	Cpu = $true; Disk = $true; Net = $true; Proto = ''; Size = '2G'; Threads = [Environment]::ProcessorCount
	Dir = (Get-Location -PSProvider FileSystem).ProviderPath; Regions = ''; Extended = $false; Quick = $false
	Board = $false; Name = ''; Share = $true; Json = $false
}
$sizeSet = $false
$argv = @($args)
for ($i = 0; $i -lt $argv.Count; $i++) {
	$a = "$($argv[$i])"
	switch -CaseSensitive ($a) {
		'-c' { $opt.Cpu = $false }
		'-s' { $opt.Disk = $false }
		'-n' { $opt.Net = $false }
		'-4' { $opt.Proto = '4' }
		'-6' { $opt.Proto = '6' }
		'-d' { $opt.Dir = "$($argv[++$i])" }
		'-S' { $opt.Size = "$($argv[++$i])".ToUpper(); $sizeSet = $true }
		'-t' { $opt.Threads = "$($argv[++$i])" }
		'-l' { $opt.Regions = "$($argv[++$i])".ToLower() -replace ' ', ',' }
		'-x' { $opt.Extended = $true }
		'-q' { $opt.Quick = $true }
		{ $_ -ceq '-L' -or $_ -ceq '--leaderboard' } {
			$opt.Board = $true
			if ($i + 1 -lt $argv.Count -and -not "$($argv[$i + 1])".StartsWith('-')) { $opt.Name = "$($argv[++$i])" }
		}
		'--no-share' { $opt.Share = $false }
		'--json' { $opt.Json = $true }
		{ $_ -ceq '-h' -or $_ -ceq '--help' } { usage; return }
		default { warn "unknown option: $a"; usage; return }
	}
}
if ($opt.Quick) {
	$FIO_TIME = 5; $IPERF_TIME = 4; $CPU_TIME = 2
	if (-not $sizeSet) { $opt.Size = '512M' }
	if ($opt.Board) {
		warn "quick runs don't go on the leaderboard, ignoring -L"
		$opt.Board = $false
	}
}
foreach ($r in ($opt.Regions -split ',' | Where-Object { $_ })) {
	if ($r -notin 'eu', 'na', 'sa', 'asia', 'oc') {
		warn "unknown region: $r (use eu, na, sa, asia, oc)"
		return
	}
}
if ($opt.Board -and -not $opt.Share) {
	warn '-L needs the upload, ignoring --no-share'
	$opt.Share = $true
}
if ($opt.Name.Length -gt 32) {
	warn 'leaderboard name is longer than 32 characters'
	return
}
if ("$($opt.Threads)" -notmatch '^\d+$' -or [int]$opt.Threads -lt 1) {
	warn "bad thread count: $($opt.Threads)"
	return
}
$opt.Threads = [int]$opt.Threads
$ui.Quiet = $opt.Json

$sys = @{}
$loc = @{}
$res = @{}
$tmp = @{ Work = Join-Path ([IO.Path]::GetTempPath()) ('nodebench.' + [IO.Path]::GetRandomFileName()) }
$clock = [Diagnostics.Stopwatch]::StartNew()
[void](New-Item -ItemType Directory -Path $tmp.Work)
try {
	say ''
	say "  $Ac$LOGO$No ${Bo}nodebench$No $Di$VERSION$No"
	say "  ${Di}cpu, disk and network benchmark  $SEP  $($URL -replace '^https?://', '')  $SEP  $((Get-Date).ToString('yyyy-MM-dd HH:mm \U\T\Czzz'))$No"

	status 'collecting system info'
	collect_system
	clear_status
	print_system

	if ($opt.Cpu) { cpu_test }
	if ($opt.Disk) { disk_test }
	if ($opt.Net) { net_test }
	print_summary

	$json = build_json
	if ($opt.Json) { $json }
	if ($opt.Share -and ($res.cpu -or $res.disk -or $res.net)) { share $json }
	say ''
} finally {
	# ctrl+c lands here too: stop a running diskspd or iperf3 first, a
	# running diskspd keeps the test file open
	clear_status
	if ($tmp.Proc -and -not $tmp.Proc.HasExited) {
		try { $tmp.Proc.Kill(); [void]$tmp.Proc.WaitForExit(5000) } catch {}
	}
	if ($tmp.File) { Remove-Item -LiteralPath $tmp.File -Force -ErrorAction SilentlyContinue }
	Remove-Item -LiteralPath $tmp.Work -Recurse -Force -ErrorAction SilentlyContinue
}
} @args
