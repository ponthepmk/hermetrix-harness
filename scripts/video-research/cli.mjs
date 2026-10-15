import {spawn} from 'node:child_process';
import path from 'node:path';
import os from 'node:os';
import {fileURLToPath} from 'node:url';
const python=process.env.HERMETRIX_RESEARCH_PYTHON||path.join(os.homedir(),'.cache/codex-runtimes/codex-primary-runtime/dependencies/python/python.exe');
const source=path.dirname(fileURLToPath(import.meta.url));
// Foreground child only: the harness's existing Job Object owns both processes.
// No detached/unref spawning or command shell; Stop terminates this owned work.
const child=spawn(python,['-X','utf8',path.join(source,'cli.py'),...process.argv.slice(2)],{stdio:'inherit',windowsHide:true,shell:false});
child.on('error',error=>{process.stderr.write(error.message+'\n');process.exitCode=1;});
child.on('exit',(code,signal)=>{process.exitCode=code??1;});
