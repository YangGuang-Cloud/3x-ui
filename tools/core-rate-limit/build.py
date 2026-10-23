#!/usr/bin/env python3
"""Build the exact go.mod Xray with a small, checked dispatcher overlay."""
import argparse, json, os, pathlib, shutil, subprocess, tempfile

MARKER = 'SorvoRateV1'

def prepare_overlay(module_dir, output):
    root = pathlib.Path(module_dir)
    source = root / 'app/dispatcher/default.go'
    text = source.read_text()
    changes = [
        ('\td.ohm = om\n', '\tif err := loadClientBandwidth(); err != nil { return err }\n\td.ohm = om\n'),
        ('\treturn inboundLink, outboundLink\n', '\tlimitClientPipes(ctx, inboundLink, outboundLink)\n\treturn inboundLink, outboundLink\n'),
        ('\treturn link\n', '\tlimitClientLink(ctx, link)\n\treturn link\n'),
    ]
    for before, after in changes:
        if text.count(before) != 1:
            raise RuntimeError('Upstream dispatcher changed; review the overlay before upgrading')
        text = text.replace(before, after, 1)
    patched = output / 'default.go'; patched.write_text(text)
    helper = pathlib.Path(__file__).resolve().with_name('dispatcher.go')
    overlay = output / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': {
        str(source): str(patched),
        str(source.with_name('sorvo_bandwidth.go')): str(helper),
    }}))
    return overlay

def main():
    parser=argparse.ArgumentParser();parser.add_argument('--output',required=True)
    args=parser.parse_args()
    module=json.loads(subprocess.check_output(['go','list','-m','-json','github.com/xtls/xray-core']))
    if not module.get('Dir'):
        module=json.loads(subprocess.check_output(['go','mod','download','-json','github.com/xtls/xray-core']))
    target=str(pathlib.Path(args.output).resolve())
    with tempfile.TemporaryDirectory(prefix='sorvo-core-') as temporary:
        temporary=pathlib.Path(temporary)
        copied=temporary/'xray-core'
        shutil.copytree(module['Dir'],copied)
        overlay=prepare_overlay(copied,temporary)
        # Apply only to a disposable copy; Go intentionally forbids overlays
        # beneath GOMODCACHE. Alternate modfile keeps the panel pin untouched.
        for original,replacement in json.loads(overlay.read_text())['Replace'].items():
            destination=pathlib.Path(original)
            destination.parent.chmod(0o755)
            if destination.exists():destination.chmod(0o644)
            shutil.copyfile(replacement,destination)
        modfile=temporary/'extension.mod'
        shutil.copyfile('go.mod',modfile)
        shutil.copyfile('go.sum',modfile.with_suffix('.sum'))
        subprocess.run(['go','mod','edit','-modfile',str(modfile),'-replace','github.com/xtls/xray-core='+str(copied)],check=True)
        subprocess.run(['go','build','-mod=mod','-modfile',str(modfile),'-trimpath','-buildvcs=false','-ldflags',f'-s -w -X github.com/xtls/xray-core/core.build={MARKER}','-o',target,'github.com/xtls/xray-core/main'],check=True)
if __name__=='__main__':main()
