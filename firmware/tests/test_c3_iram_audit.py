"""Final-linked ELF tests; no target toolchain or external ELF package needed."""
import importlib.util
import os
import json
from pathlib import Path
import struct
import tempfile
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

    def test_flash_code_and_unknown_indirect_fail_closed(self):
        code=struct.pack('<II',0x420002b7,0x000280e7)+b'\x82\x80'
        with self.assertRaisesRegex(ValueError,'Flash|unsafe'):
            self.module.Audit(self.module.ELF(elf_fixture(code,callee=0x42000000,target_section='.flash.text',reloc_type=19))).closure(['entry'])
        with self.assertRaisesRegex(ValueError,'indirect'):
            self.module.Audit(self.module.ELF(elf_fixture(b'\x02\x95',reloc_type=51))).closure(['entry'])

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
