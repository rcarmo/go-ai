#!/usr/bin/env python3
"""Retain Python call/allocation profiles, including Python subprocess helpers."""
from __future__ import annotations

import cProfile
import io
import os
from pathlib import Path
import pstats
import runpy
import shutil
import subprocess
import sys
import tempfile
import traceback
import tracemalloc

sys.dont_write_bytecode = True
from project_temp import configure


def main() -> int:
    configure("python-helpers")
    if len(sys.argv) < 2:
        raise SystemExit("usage: profile-python.py script.py [args...]")
    target = Path(sys.argv[1]).resolve()
    args = sys.argv[2:]
    evidence = Path(os.environ.get("GO_AI_PYTHON_PROFILE_ROOT", str(Path(os.environ["PROJECT_TMP_ROOT"]) / "evidence" / "python")))
    # Isolation probes may deliberately override the child scratch root; retain
    # their profiles in the parent's evidence root outside that disposable tree.
    os.environ["GO_AI_PYTHON_PROFILE_ROOT"] = str(evidence)
    evidence.mkdir(parents=True, exist_ok=True)
    run = Path(tempfile.mkdtemp(prefix=target.stem + "-", dir=evidence))
    (run / "source.py").write_bytes(target.read_bytes())
    (run / "invocation.txt").write_text(f"python={sys.version}\nscript={target}\nargs={args!r}\nroot={os.environ['PROJECT_TMP_ROOT']}\n")
    # Each Python subprocess gets its own wrapper/profile; Go generator children
    # use their own pprof capture and retained executable in the helper run.
    original_popen = subprocess.Popen
    wrapper = str(Path(__file__).resolve())

    def profiled_popen(command, *pargs, **kwargs):
        if isinstance(command, (tuple, list)) and command:
            command = list(command)
            executable = str(command[0])
            if (Path(executable).name.startswith("python") and len(command) > 1
                    and str(command[1]).endswith(".py") and str(command[1]) != wrapper):
                command.insert(1, wrapper)
            elif executable.endswith(".py") and executable != wrapper:
                command = [sys.executable, wrapper, *command]
        return original_popen(command, *pargs, **kwargs)

    subprocess.Popen = profiled_popen
    sys.path.insert(0, str(target.parent))
    sys.argv = [str(target), *args]
    profiler = cProfile.Profile()
    tracemalloc.start(25)
    profiler.enable()
    status = 0
    try:
        runpy.run_path(str(target), run_name="__main__")
    except SystemExit as exc:
        status = exc.code if isinstance(exc.code, int) else 1 if exc.code else 0
        if not isinstance(exc.code, (int, type(None))):
            print(exc.code, file=sys.stderr)
    except BaseException:
        traceback.print_exc()
        status = 1
    finally:
        profiler.disable()
        current, peak = tracemalloc.get_traced_memory()
        snapshot = tracemalloc.take_snapshot()
        snapshot.dump(str(run / "heap.tracemalloc"))
        tracemalloc.stop()
        profiler.dump_stats(str(run / "cpu.cprofile"))
        output = io.StringIO()
        pstats.Stats(profiler, stream=output).sort_stats("cumulative").print_stats(35)
        (run / "cpu.txt").write_text(output.getvalue())
        allocations = snapshot.statistics("traceback")
        for metric, order in (("alloc_space", "size"), ("alloc_objects", "count")):
            rows = sorted(allocations, key=lambda stat: getattr(stat, order), reverse=True)
            report = f"Live traced bytes={current}; peak={peak}. Snapshot is retained allocations, not cumulative churn.\n"
            for stat in rows[:35]:
                report += f"{stat.size} bytes / {stat.count} objects: {stat.traceback}\n"
            (run / f"{metric}.txt").write_text(report)
        summary = evidence.parent / "analysis" / (run.name + ".txt")
        summary.parent.mkdir(parents=True, exist_ok=True)
        summary.write_text(f"Python script={target}; status={status}; live={current}; peak={peak}.\nSnapshot allocations are retained bytes/objects, not cumulative churn.\n" + output.getvalue()[:6000] + "\n" + "\n".join(str(stat) for stat in allocations[:10]))
        print(f"Python analysis: {summary}; status={status}; peak traced bytes={peak}", file=sys.stderr)
        shutil.rmtree(run) # Analysis consumed captures: dispose immediately.
    return status


if __name__ == "__main__":
    raise SystemExit(main())
