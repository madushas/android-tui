package core

import ()

type LogPreset struct {
	Name, Regex string
	MinLevel    int // index into VDIWEFS
}

var LogPresets = []LogPreset{
	{"Crashes", `FATAL EXCEPTION|AndroidRuntime|Fatal signal|SIGSEGV|SIGABRT|Process: .* has died|tombstone|backtrace:`, 0},
	{"ANRs", `ANR in|am_anr|Input dispatching timed out|not responding|Reason: Input|executing service`, 0},
	{"GC pauses", `GC freed|Background concurrent|concurrent copying|Paused GC|Explicit concurrent|Starting a blocking GC`, 0},
	{"Low memory / process kills", `lmkd|lowmemorykiller|Killing .* \(adj|Phantom|am_kill|am_proc_died|OOM`, 0},
	{"Battery saver / Doze", `BatterySaver|PowerManagerService.*(low power|battery saver)|DeviceIdleController|deep idle|light idle|Doze`, 0},
	{"Wakelocks", `WakeLock|wake_lock|acquire.*wakelock|release.*wakelock`, 0},
	{"Job scheduler", `JobScheduler|JobServiceContext|jobscheduler|StandbyBucket|AppStandby`, 0},
	{"Warnings and errors only", `.`, 3},
}
