"""Final-linked ELF tests; no target toolchain or external ELF package needed."""
import importlib.util
import os
import json
from pathlib import Path
import struct
import shutil
import subprocess
import tempfile
import textwrap
import unittest
from unittest.mock import patch

SCRIPT = Path(__file__).resolve().parents[1]/'overlays/scripts/family_c3_iram_audit.py'


def elf_fixture(code=b'\x82\x80', *, callee=0x40380100, target_section='.iram0.text', reloc_type=17,
                data_pointer=None,pcrel=False,absolute_rom=False):
    # ELF32/LE/RISC-V with real symbols and an emitted RELA section.
    names=b'\0.shstrtab\0.strtab\0.symtab\0.iram0.text\0.rela.iram0.text\0.flash.text\0.dram0.data\0.rela.dram0.data\0'
    strings=b'\0entry\0callee\0literal\0.Lhi\0'
    symbols=bytes(16)+struct.pack('<IIIBBH',1,0x40380000,len(code),0x12,0,4)+struct.pack('<IIIBBH',7,callee,2,0x12,0,
        0xfff1 if absolute_rom else 4 if target_section=='.iram0.text' else 6)
    reloc=struct.pack('<IIi',0x40380000,(2<<8)|reloc_type,0)
    if data_pointer is not None:
        symbols+=struct.pack('<IIIBBH',14,0x3fc80000,4,0x11,0,7)
        reloc=struct.pack('<IIi',0x40380000,(3<<8)|(23 if pcrel else 26),0)
        if pcrel:
            symbols+=struct.pack('<IIIBBH',22,0x40380000,0,0,0,4)
            reloc+=struct.pack('<IIi',0x40380004,(4<<8)|24,0)
    text=code.ljust(0x100,b'\0')+b'\x82\x80'
    sections=[(0,0,0,0,b'',0,0,0),
        (names.index(b'.shstrtab'),3,0,0,names,0,0,0),
        (names.index(b'.strtab'),3,0,0,strings,0,0,0),
        (names.index(b'.symtab'),2,0,0,
         symbols,2,1,16),
        (names.index(b'.iram0.text'),1,6,0x40380000,text,0,0,0),
        (names.index(b'.rela.iram0.text'),4,0,0,
         reloc,3,4,12),
        (names.index(b'.flash.text'),1,6,0x42000000,b'\x82\x80',0,0,0)]
    if data_pointer is not None:
        sections.extend([(names.index(b'.dram0.data'),1,3,0x3fc80000,struct.pack('<I',data_pointer),0,0,0),
            (names.index(b'.rela.dram0.data'),4,0,0,struct.pack('<IIi',0x3fc80000,(2<<8)|1,0),3,7,12)])
    blob=bytearray(52); headers=[]
    for name,typ,flags,addr,data,link,info,entsize in sections:
        offset=len(blob);blob.extend(data);headers.append((name,typ,flags,addr,offset,len(data),link,info,1,entsize))
    shoff=len(blob)
    for header in headers:blob.extend(struct.pack('<IIIIIIIIII',*header))
    struct.pack_into('<16sHHIIIIIHHHHHH',blob,0,b'\x7fELF\x01\x01\x01'+bytes(9),2,243,1,0x40380000,0,shoff,1,52,0,0,40,len(sections),1)
    return bytes(blob)


class IramAuditTests(unittest.TestCase):
    def setUp(self):
        self.assertTrue(SCRIPT.is_file(),'final ELF analyzer is missing')
        spec=importlib.util.spec_from_file_location('iram_audit',SCRIPT)
        self.module=importlib.util.module_from_spec(spec);spec.loader.exec_module(self.module)

    def test_real_elf_parser_and_compressed_return(self):
        elf=self.module.ELF(elf_fixture())
        self.assertEqual(elf.symbol('entry')['address'],0x40380000)
        self.assertEqual(self.module.decode(b'\x82\x80',0)['kind'],'return')

    def test_relaxed_jal_edges_are_checked_against_final_instructions(self):
        # jal ra,+256
        code=struct.pack('<I',0x100000ef)+b'\x82\x80'
        report=self.module.Audit(self.module.ELF(elf_fixture(code))).closure(['entry'])
        self.assertIn('callee',[x['name'] for x in report['code']])
        with self.assertRaisesRegex(ValueError,'relocation.*instruction'):
            self.module.Audit(self.module.ELF(elf_fixture(code,callee=0x40380102))).closure(['entry'])
        for kind in (18,19):
            self.module.Audit(self.module.ELF(elf_fixture(code,reloc_type=kind))).closure(['entry'])
        far=struct.pack('<II',0x00000317,0x100300e7)+b'\x82\x80'
        self.module.Audit(self.module.ELF(elf_fixture(far,reloc_type=19))).closure(['entry'])

    def test_data_pointer_literals_and_pcrel_pairs_match_actual_bytes(self):
        absolute=struct.pack('<I',0x3fc802b7)+b'\x82\x80'
        self.module.Audit(self.module.ELF(elf_fixture(absolute,data_pointer=0x40380100))).closure(['entry'])
        with self.assertRaisesRegex(ValueError,'pointer relocation'):
            self.module.Audit(self.module.ELF(elf_fixture(absolute,data_pointer=0x42000000))).closure(['entry'])
        pcrel=struct.pack('<II',0xff900297,0x00028293)+b'\x82\x80'
        self.module.Audit(self.module.ELF(elf_fixture(pcrel,data_pointer=0x40380100,pcrel=True))).closure(['entry'])
        with self.assertRaisesRegex(ValueError,'PCREL low'):
            self.module.Audit(self.module.ELF(elf_fixture(pcrel[:4]+struct.pack('<I',0x00428293)+pcrel[8:],data_pointer=0x40380100,pcrel=True))).closure(['entry'])

    def test_exact_indirect_field_contract_and_control_flow_join(self):
        code=struct.pack('<II',0x01062783,0x000780e7)+b'\x82\x80'
        policy={('entry',repr(('load',('argument',2),16))):(0x40380100,'fixture exact callback field')}
        result=self.module.Audit(self.module.ELF(elf_fixture(code,reloc_type=51)),policy).closure(['entry'])
        self.assertEqual(len(result['indirect']),1)
        # One path bypasses the load; a linear scan would wrongly allow it.
        branch=struct.pack('<III',0x00050463,0x01062783,0x000780e7)+b'\x82\x80'
        with self.assertRaisesRegex(ValueError,'indirect'):
            self.module.Audit(self.module.ELF(elf_fixture(branch,reloc_type=51)),policy).closure(['entry'])

    def test_timer_style_spilled_callback_survives_a_call_without_guessing(self):
        code=struct.pack('<IIIIII',0x01062783,0xff010113,0x00f12623,0x0f4000ef,0x00c12783,0x01010113)+b'\x82\x87'
        policy={('entry',repr(('load',('argument',2),16))):(0x40380100,'fixture exact timer-style saved field')}
        result=self.module.Audit(self.module.ELF(elf_fixture(code,reloc_type=51)),policy).closure(['entry'])
        self.assertEqual(len(result['indirect']),1)

    def test_rom_requires_the_reviewed_absolute_symbol(self):
        code=struct.pack('<II',0x400002b7,0x354280e7)+b'\x82\x80'
        good=elf_fixture(code,callee=0x40000354,reloc_type=19,absolute_rom=True).replace(b'callee\0',b'memset\0')
        result=self.module.Audit(self.module.ELF(good)).closure(['entry'])
        self.assertTrue(any(entry.get('rom') for entry in result['code']))
        with self.assertRaisesRegex(ValueError,'memset'):
            self.module.Audit(self.module.ELF(good.replace(b'memset\0',b'calloc\0'))).closure(['entry'])

    def test_native_link_capture_expands_response_files_and_rejects_ambiguity(self):
        command='c++ -Wl,--emit-relocs -Wl,--wrap=guard -o xiaozhi.elf main.a'
        rows=[dict(output='main.cc.obj',command='c++ -c main.cc'),dict(output='xiaozhi.elf',command=command)]
        self.assertEqual(self.module.select_link_command(json.dumps(rows)),command+'\n')
        for entries in [rows+rows,[dict(output='xiaozhi.elf',command='c++ @missing.rsp -o xiaozhi.elf')],[]]:
            with self.assertRaises(ValueError):self.module.select_link_command(json.dumps(entries))

    def test_sdk_source_retention_and_report_are_independently_rechecked(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);tree=root/'tree';(tree/'build').mkdir(parents=True)
            sdk=root/'sdk';sdk.mkdir();source=sdk/'sdk.c';source.write_bytes(b'fixed source\n')
            pins={'sdk.c':self.module.sha(source.read_bytes())}
            with patch.object(self.module,'source_pins',return_value=pins):
                self.module.capture(tree,sdk,'actual expanded link\n')
                self.assertEqual((tree/'build/family_c3_cache/sdk/sdk.c').read_bytes(),source.read_bytes())
                source.write_bytes(b'drift')
                with self.assertRaisesRegex(ValueError,'drift'):self.module.capture(tree,sdk,'bad link\n')
            report=tree/'build/family_c3_cache/iram-audit.json'
            report.write_text(json.dumps({'verified':1},indent=2,sort_keys=True)+'\n')
            with patch.object(self.module,'audit_tree',return_value={'verified':1}):
                self.module.verify_report(tree)
                report.write_text('{"verified":2}')
                with self.assertRaisesRegex(ValueError,'recomputation'):self.module.verify_report(tree)

    def test_failed_c3_job_retains_only_debug_inputs(self):
        workflow=(Path(__file__).resolve().parents[2]/'.github/workflows/firmware-experiment.yml').read_text()
        marker='      - name: Retain failed C3 build diagnostics only\n'
        self.assertIn(marker,workflow)
        step=workflow.split(marker,1)[1].split('\n      - ',1)[0]
        self.assertIn("if: ${{ failure() && matrix.variant == 'xmini-c3-family' }}",step)
        self.assertIn('uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9',step)
        self.assertIn('name: c3-build-debug-only-${{ github.sha }}',step)
        self.assertIn('if-no-files-found: warn',step)
        paths={line.strip() for line in step.split('path: |\n',1)[1].splitlines() if line.strip()}
        self.assertEqual(paths,{'firmware/.cache/experiment/build/xiaozhi.elf',
            'firmware/.cache/experiment/build/xiaozhi.map','firmware/.cache/experiment/sdkconfig',
            'firmware/.cache/experiment/build/compile_commands.json','firmware/.cache/experiment/build/family_c3_cache/',
            'firmware/.cache/experiment/build/esp-idf/main/libmain.a',
            'firmware/.cache/experiment/build/CMakeCache.txt','firmware/.cache/experiment/build/build.ninja'})
        for forbidden in ('xiaozhi.bin','merged-binary.bin','flash_args','family_c3_pcm/'):
            self.assertNotIn(forbidden,step)

    def test_failed_c3_section_capture_uses_only_unique_cached_tool_and_records_missing_or_failure(self):
        workflow=(Path(__file__).resolve().parents[2]/'.github/workflows/firmware-experiment.yml').read_text()
        marker='      - name: Capture failed C3 archive sections\n'
        self.assertIn(marker,workflow)
        step=workflow.split(marker,1)[1].split('\n      - ',1)[0]
        self.assertIn("if: ${{ failure() && matrix.variant == 'xmini-c3-family' }}",step)
        self.assertIn('continue-on-error: true',step)
        script=textwrap.dedent(step.split('run: |\n',1)[1])
        objdump=Path(shutil.which('objdump')).resolve()
        for case in ('captured','archive_missing','cache_missing','cache_ambiguous','objdump_failed'):
            with self.subTest(case=case),tempfile.TemporaryDirectory() as temporary:
                root=Path(temporary);build=root/'firmware/.cache/experiment/build'
                archive=build/'esp-idf/main/libmain.a';archive.parent.mkdir(parents=True)
                tool=Path(shutil.which('false')).resolve() if case=='objdump_failed' else objdump
                cache=build/'CMakeCache.txt';row=f'CMAKE_OBJDUMP:FILEPATH={tool}\n'
                if case!='cache_missing':cache.write_text(row*(2 if case=='cache_ambiguous' else 1))
                (build/'build.ninja').write_text('# actual build binding fixture\n')
                if case!='archive_missing':
                    source=root/'probe.c';source.write_text('int section_capture_probe;\n')
                    subprocess.run(['cc','-c',str(source),'-o',str(root/'probe.o')],check=True,capture_output=True)
                    subprocess.run(['ar','rcs',str(archive),str(root/'probe.o')],check=True,capture_output=True)
                result=subprocess.run(['bash','-c',script],cwd=root,text=True,capture_output=True,
                                      env=dict(os.environ,LC_ALL='POSIX'),timeout=30)
                self.assertEqual(result.returncode,0,result.stdout+result.stderr)
                out=build/'family_c3_cache/ldgen-debug'
                report=json.loads((out/'capture.json').read_text())
                expected='capture_error' if case.startswith('cache_') else case
                self.assertEqual(report['status'],expected)
                self.assertEqual(report['locale'],'C')
                self.assertEqual(report['inputs']['build.ninja']['sha256'],
                                 self.module.sha((build/'build.ninja').read_bytes()))
                if case.startswith('cache_'):
                    self.assertIn('CMAKE_OBJDUMP',report['error'])
                    self.assertFalse((out/'main-sections.txt').exists())
                    continue
                self.assertEqual(report['tool']['configured_path'],str(tool))
                self.assertEqual(report['tool']['sha256'],self.module.sha(tool.read_bytes()))
                self.assertTrue((out/'objdump-version.txt').is_file())
                if case=='archive_missing':
                    self.assertFalse(report['inputs']['esp-idf/main/libmain.a']['exists'])
                    self.assertIsNone(report['dump_exit_code'])
                    self.assertFalse((out/'main-sections.txt').exists())
                else:
                    code=1 if case=='objdump_failed' else 0
                    self.assertEqual(report['dump_exit_code'],code)
                    self.assertEqual((out/'main-sections.exit.txt').read_text().strip(),str(code))
                    self.assertTrue((out/'main-sections.stderr.txt').is_file())
                    self.assertEqual(report['inputs']['esp-idf/main/libmain.a']['sha256'],
                                     self.module.sha(archive.read_bytes()))
                    if case=='captured':self.assertIn('In archive',(out/'main-sections.txt').read_text())

    def test_flash_code_and_unknown_indirect_fail_closed(self):
        code=struct.pack('<II',0x420002b7,0x000280e7)+b'\x82\x80'
        with self.assertRaisesRegex(ValueError,'Flash|unsafe'):
            self.module.Audit(self.module.ELF(elf_fixture(code,callee=0x42000000,target_section='.flash.text',reloc_type=19))).closure(['entry'])
        with self.assertRaisesRegex(ValueError,'indirect'):
            self.module.Audit(self.module.ELF(elf_fixture(b'\x02\x95',reloc_type=51))).closure(['entry'])

    @staticmethod
    def access(address,width,store=False,unsigned=False):
        high=(address+0x800)&0xfffff000;low=address-high
        first=high|(5<<7)|0x37
        funct={1:0,2:1,4:2}[width]+(4 if unsigned and width<4 else 0)
        if store:
            second=((low&0xfe0)<<20)|(15<<20)|(5<<15)|(funct<<12)|((low&31)<<7)|0x23
        else:second=((low&0xfff)<<20)|(5<<15)|(funct<<12)|(15<<7)|3
        return struct.pack('<II',first,second)+b'\x82\x80'

    def test_actual_constant_memory_accesses_reject_flash_without_relocations(self):
        for address in (0x3c000000,0x42000000):
            for width in (1,2,4):
                for store in (False,True):
                    with self.subTest(address=hex(address),width=width,store=store),self.assertRaisesRegex(ValueError,'memory|data|Flash'):
                        self.module.Audit(self.module.ELF(elf_fixture(self.access(address,width,store),reloc_type=51))).closure(['entry'])
                if width<4:
                    with self.assertRaises(ValueError):
                        self.module.Audit(self.module.ELF(elf_fixture(self.access(address,width,unsigned=True),reloc_type=51))).closure(['entry'])

    def test_memory_width_boundaries_compressed_and_pcrel_are_checked(self):
        for address,width in ((0x3fc80000,1),(0x3fcdffff,1),(0x3fcdfffe,2),(0x3fcdfffc,4)):
            result=self.module.Audit(self.module.ELF(elf_fixture(self.access(address,width),reloc_type=51))).closure(['entry'])
            self.assertEqual([(item['address'],item['width']) for item in result['memory_accesses']],[(address,width)])
        for address,width in ((0x3fc7ffff,1),(0x3fce0000,1),(0x3fcdffff,2),(0x3fcdfffe,4),(0xfffffffe,4)):
            with self.assertRaises(ValueError):
                self.module.Audit(self.module.ELF(elf_fixture(self.access(address,width),reloc_type=51))).closure(['entry'])
        for base,half in ((8,0x4004),(8,0xc004),(2,0x4782),(2,0xc03e)):
            code=struct.pack('<IH',0x3c000037|(base<<7),half)+b'\x82\x80'
            with self.assertRaises(ValueError):self.module.Audit(self.module.ELF(elf_fixture(code,reloc_type=51))).closure(['entry'])
        # AUIPC-derived DROM address with no address relocation is still unsafe.
        code=struct.pack('<II',0xfbc80297,0x0002a783)+b'\x82\x80'
        with self.assertRaises(ValueError):self.module.Audit(self.module.ELF(elf_fixture(code,reloc_type=51))).closure(['entry'])
        for code in (struct.pack('<III',0x3c0002b7,0x00428293,0x0002a783)+b'\x82\x80',
                     struct.pack('<IHH',0x3c000437,0x0411,0x4004)+b'\x82\x80'):
            with self.assertRaises(ValueError):self.module.Audit(self.module.ELF(elf_fixture(code,reloc_type=51))).closure(['entry'])
        pcrel=struct.pack('<II',0xff900297,0x0002a783)+b'\x82\x80'
        result=self.module.Audit(self.module.ELF(elf_fixture(pcrel,data_pointer=0x40380100,pcrel=True))).closure(['entry'])
        self.assertEqual([(item['address'],item['width']) for item in result['memory_accesses']],[(0x3fc80000,4)])

    def test_mmio_and_unknown_bases_require_specific_contracts(self):
        address=0x60023000;code=self.access(address,4)
        with self.assertRaises(ValueError):self.module.Audit(self.module.ELF(elf_fixture(code,reloc_type=51))).closure(['entry'])
        contract={('entry',address,4,'load'):'fixture exact register'}
        self.module.Audit(self.module.ELF(elf_fixture(code,reloc_type=51)),mmio_contracts=contract).closure(['entry'])
        for wrong in (self.access(address+4,4),self.access(address,2),self.access(address,4,store=True)):
            with self.assertRaises(ValueError):self.module.Audit(self.module.ELF(elf_fixture(wrong,reloc_type=51)),mmio_contracts=contract).closure(['entry'])
        # Unsupported pointer arithmetic loses its model; a source contract
        # cannot turn an unmodelled register into a claimed internal address.
        unknown=struct.pack('<II',0x00b502b3,0x0002a783)+b'\x82\x80'
        with self.assertRaisesRegex(ValueError,'addressing model'):
            self.module.Audit(self.module.ELF(elf_fixture(unknown,reloc_type=51)),memory_contracts={'entry':'fixture internal context'}).closure(['entry'])

    def test_unsupported_memory_encodings_are_rejected(self):
        for instruction in (0x0002b783,0x00f2b023,0x0002a787,0x00f2a027,0x0002a7af):
            with self.subTest(instruction=hex(instruction)),self.assertRaisesRegex(ValueError,'unsupported.*memory'):
                self.module.Audit(self.module.ELF(elf_fixture(struct.pack('<I',instruction)+b'\x82\x80',reloc_type=51))).closure(['entry'])

    def test_callback_contract_requires_word_load_and_zero_jalr_immediate(self):
        policy={('entry',repr(('load',('argument',2),16))):(0x40380100,'fixture exact callback field')}
        for immediate in (2,4,-2):
            code=struct.pack('<II',0x01062783,((immediate&0xfff)<<20)|0x000780e7)+b'\x82\x80'
            for target in (0x40380100,None):
                with self.subTest(immediate=immediate,target=target),self.assertRaisesRegex(ValueError,'JALR.*immediate'):
                    self.module.Audit(self.module.ELF(elf_fixture(code,reloc_type=51)),{next(iter(policy)):(target,'fixture field')}).closure(['entry'])
        for load in (0x01064783,0x01065783): # LBU/LHU cannot supply a callback pointer
            code=struct.pack('<II',load,0x000780e7)+b'\x82\x80'
            with self.assertRaisesRegex(ValueError,'indirect'):
                self.module.Audit(self.module.ELF(elf_fixture(code,reloc_type=51)),policy,
                    memory_contracts={'entry':'fixture checked internal context'}).closure(['entry'])

    def test_malformed_headers_and_missing_relocations_are_rejected(self):
        for source in [b'ELF',elf_fixture()[:100],b'\x7fELF\x02'+elf_fixture()[5:]]:
            with self.assertRaises(ValueError):self.module.ELF(source)

    @unittest.skipUnless(os.environ.get('C3_CACHE_RED_BUNDLE'),'exact ee4249d bundle required')
    def test_actual_ee_candidate_is_rejected_for_independent_contract_gaps(self):
        root=Path(os.environ['C3_CACHE_RED_BUNDLE'])
        failures=self.module.baseline_rejections(root)
        for reason in ['IRAM config','Flash callback','missing wrapper','missing emitted relocations']:
            self.assertTrue(any(reason in failure for failure in failures),(reason,failures))
        print('actual ee4249d rejected:',failures)
