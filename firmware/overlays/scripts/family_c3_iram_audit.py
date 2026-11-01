#!/usr/bin/env python3
"""Fail-closed audit of FINAL linked C3 code, data, relocations and guard edges.

Only Python's standard library is used. The retained report can be reproduced
without the builder's IDF path, objdump, toolchain, or network. Unknown dynamic
calls are failures; source-backed callback exclusions are recorded per callsite.
"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import shlex
import struct
import subprocess

SDK_COMMIT = 'fff9895c82d744c7237be8847347bdd1b07c6643'
IRAM = (0x40380000, 0x403e0000)
DRAM = (0x3fc80000, 0x3fce0000)
ROM = {'memset': 0x40000354, '__divdi3': 0x400007b4, '__moddi3': 0x4000083c,
       '__udivdi3': 0x400008ac, '__udivmoddi4': 0x400008b0, '__umoddi3': 0x400008bc}
DISABLE = 'spi_flash_disable_interrupts_caches_and_other_cpu'
ENABLE = 'spi_flash_enable_interrupts_caches_and_other_cpu'
CALLBACKS = ('_ZN13FamilyC3Codec4SentEP17i2s_channel_obj_tP16i2s_event_data_tPv',
             '_ZN13FamilyC3Codec8ReceivedEP17i2s_channel_obj_tP16i2s_event_data_tPv')
APPLICATION_INPUTS = ('main/boards/xmini/c3/family_c3_isr.cc',
    'main/boards/xmini/c3/family_c3_isr.inc','main/boards/xmini/c3/family_c3_dma.inc',
    'main/boards/xmini/c3/family_c3_flash_wrap.inc','main/boards/xmini/c3/family_c3_codec.h',
    'main/boards/xmini/c3/family_c3_sdk_layout.c','main/boards/xmini/c3/family_c3_isr.lf',
    'main/audio/c3_cache_span.h','main/audio/c3_cache_trace.h','main/audio/c3_startup_diagnostics.h',
    'main/audio/playback_progress.h','main/audio/playback_reference_buffer.h',
    'main/boards/m5stack/stopwatch/duplex_reference.h')


def sha(data):
    return hashlib.sha256(data).hexdigest()


def regular(path):
    path = Path(path)
    if not path.is_file() or path.is_symlink() or any(p.is_symlink() for p in path.parents):
        raise ValueError('missing/nonregular native input: '+str(path))
    return path.read_bytes()


class ELF:
    def __init__(self, data):
        self.data = data
        if len(data) < 52 or data[:7] != b'\x7fELF\x01\x01\x01':
            raise ValueError('requires ELF32 little-endian version 1')
        header = struct.unpack_from('<16sHHIIIIIHHHHHH', data)
        if header[1:4] != (2, 243, 1) or header[8] != 52 or header[11] != 40:
            raise ValueError('requires final executable RISC-V ELF')
        shoff, count, names = header[6], header[12], header[13]
        if not count or names >= count or shoff+40*count > len(data):
            raise ValueError('invalid section table')
        self.sections = []
        for i in range(count):
            row = struct.unpack_from('<IIIIIIIIII', data, shoff+40*i)
            section = dict(zip(('name_offset','type','flags','address','offset','size','link','info','align','entsize'),row))
            if section['type'] != 8 and section['offset']+section['size'] > len(data):
                raise ValueError('section exceeds ELF')
            self.sections.append(section)
        strings = self.section_bytes(names)
        for section in self.sections:
            section['name'] = self.string(strings,section['name_offset'])
        self.symbols, self.tables = [], {}
        for i, section in enumerate(self.sections):
            if section['type'] != 2:
                continue
            if section['entsize'] != 16 or section['size'] % 16 or section['link'] >= count:
                raise ValueError('invalid symbol table')
            strings = self.section_bytes(section['link']); table = []
            for offset in range(section['offset'],section['offset']+section['size'],16):
                name, address, size, info, other, index = struct.unpack_from('<IIIBBH',data,offset)
                symbol = dict(name=self.string(strings,name),address=address,size=size,type=info&15,
                              binding=info>>4,section=index)
                if index < 0xff00 and index >= count:
                    raise ValueError('symbol section out of range')
                table.append(symbol);self.symbols.append(symbol)
            self.tables[i] = table
        if not self.tables:
            raise ValueError('final ELF symbols missing')
        self.relocs = []
        for section in self.sections:
            if section['type'] != 4:
                continue
            if section['entsize'] != 12 or section['size'] % 12 or section['link'] not in self.tables or section['info'] >= count:
                raise ValueError('invalid emitted relocation section')
            target = self.sections[section['info']]
            if not target['flags'] & 2:
                continue
            for offset in range(section['offset'],section['offset']+section['size'],12):
                address, info, addend = struct.unpack_from('<IIi',data,offset)
                table = self.tables[section['link']]
                if info>>8 >= len(table) or not target['address'] <= address < target['address']+target['size']:
                    raise ValueError('emitted relocation outside owning section')
                self.relocs.append(dict(address=address,type=info&255,symbol=table[info>>8],addend=addend))

    @staticmethod
    def string(data, offset):
        if offset >= len(data) or data.find(b'\0',offset) < 0:
            raise ValueError('invalid ELF string')
        return data[offset:data.index(0,offset)].decode('utf-8')

    def section_bytes(self,index):
        s=self.sections[index];return self.data[s['offset']:s['offset']+s['size']]

    def symbol(self,name):
        matches=[s for s in self.symbols if s['name']==name and s['section']]
        if len(matches)!=1:
            raise ValueError('missing/ambiguous native symbol: '+name)
        return matches[0]

    def containing(self,address,kind=None):
        matches=[s for s in self.symbols if s['section'] and s['size'] and
                 s['address']<=address<s['address']+s['size'] and (kind is None or s['type']==kind)]
        if not matches:
            return None
        return min(matches,key=lambda s:(s['size'],s['name']))

    def read(self,address,size):
        for s in self.sections:
            if s['flags']&2 and s['address']<=address and address+size<=s['address']+s['size']:
                if s['type']==8:
                    return bytes(size)
                at=s['offset']+address-s['address'];return self.data[at:at+size]
        raise ValueError('unmapped native bytes: '+hex(address))


def signed(value,bits):
    return (value^(1<<(bits-1)))-(1<<(bits-1))


def decode(data,pc):
    if len(data)<2:
        raise ValueError('truncated RISC-V instruction')
    h=int.from_bytes(data[:2],'little');d=dict(size=2,kind='other',writes=None)
    if h&3 != 3:
        group,op=h&3,h>>13
        if group==1 and op in (1,5):
            imm=((h>>12&1)<<11)|((h>>11&1)<<4)|((h>>9&3)<<8)|((h>>8&1)<<10)|((h>>7&1)<<6)|((h>>6&1)<<7)|((h>>3&7)<<1)|((h>>2&1)<<5)
            d.update(kind='direct',target=(pc+signed(imm,12))&0xffffffff,rd=1 if op==1 else 0)
        elif group==1 and op in (6,7):
            imm=((h>>12&1)<<8)|((h>>10&3)<<3)|((h>>5&3)<<6)|((h>>3&3)<<1)|((h>>2&1)<<5)
            d.update(kind='branch',target=(pc+signed(imm,9))&0xffffffff)
        elif group==2 and op==4:
            rd,rs2=(h>>7)&31,(h>>2)&31
            if not rs2 and rd:
                d.update(kind='return' if rd==1 and not h&0x1000 else 'indirect',rs=rd,imm=0,rd=1 if h&0x1000 else 0)
            elif rs2 and not h&0x1000:
                d.update(kind='move',rd=rd,rs=rs2,writes=rd)
            elif rs2:
                d.update(writes=rd)
        elif group==1 and op in (0,2,3):
            rd=(h>>7)&31;imm=signed(((h>>12&1)<<5)|((h>>2)&31),6)
            if op==0:
                d.update(kind='addi',rd=rd,rs=rd,imm=imm,writes=rd)
            elif op==2:
                d.update(kind='constant',rd=rd,value=imm,writes=rd)
            elif rd not in (0,2):
                d.update(kind='constant',rd=rd,value=(imm<<12)&0xffffffff,writes=rd)
            elif rd==2:
                imm=((h>>12&1)<<9)|((h>>6&1)<<4)|((h>>5&1)<<6)|((h>>3&3)<<7)|((h>>2&1)<<5)
                d.update(kind='addi',rd=2,rs=2,imm=signed(imm,10),writes=2)
            else:d.update(writes=rd)
        elif group==0 and op==2:
            d.update(kind='load',rd=8+((h>>2)&7),rs=8+((h>>7)&7),
                     imm=((h>>10&7)<<3)|((h>>6&1)<<2)|((h>>5&1)<<6),writes=8+((h>>2)&7))
        elif group==2 and op==2:
            d.update(kind='load',rd=(h>>7)&31,rs=2,
                     imm=((h>>12&1)<<5)|((h>>4&7)<<2)|((h>>2&3)<<6),writes=(h>>7)&31)
        elif group==2 and op==6:
            d.update(kind='store',rs=2,value_rs=(h>>2)&31,imm=((h>>9&15)<<2)|((h>>7&3)<<6))
        elif group==0 and op==6:
            d.update(kind='store',rs=8+((h>>7)&7),value_rs=8+((h>>2)&7),
                     imm=((h>>10&7)<<3)|((h>>6&1)<<2)|((h>>5&1)<<6))
        # Other compressed arithmetic may change a register: invalidate it.
        elif group==1 and op==4:
            d.update(writes=8+((h>>7)&7))
        elif group==2 and op==0:
            d.update(writes=(h>>7)&31)
        elif group==0 and op==0:
            d.update(kind='addi',rd=8+((h>>2)&7),rs=2,writes=8+((h>>2)&7),
                     imm=((h>>7&15)<<6)|((h>>11&3)<<4)|((h>>5&1)<<3)|((h>>6&1)<<2))
        return d
    if len(data)<4:
        raise ValueError('truncated 32-bit RISC-V instruction')
    w=int.from_bytes(data[:4],'little');op=w&127;rd=(w>>7)&31;rs=(w>>15)&31;f=(w>>12)&7
    d.update(size=4)
    if op==0x6f:
        imm=((w>>31)<<20)|((w>>21&1023)<<1)|((w>>20&1)<<11)|((w>>12&255)<<12)
        d.update(kind='direct',rd=rd,target=(pc+signed(imm,21))&0xffffffff)
    elif op==0x63:
        imm=((w>>31)<<12)|((w>>25&63)<<5)|((w>>8&15)<<1)|((w>>7&1)<<11)
        d.update(kind='branch',target=(pc+signed(imm,13))&0xffffffff)
    elif op==0x67:
        imm=signed(w>>20,12)
        d.update(kind='return' if rs==1 and rd==0 and imm==0 else 'indirect',rd=rd,rs=rs,imm=imm)
    elif op in (0x37,0x17):
        d.update(kind='constant',rd=rd,value=((w&0xfffff000)+(pc if op==0x17 else 0))&0xffffffff,writes=rd)
    elif op==0x13 and f==0:
        d.update(kind='addi',rd=rd,rs=rs,imm=signed(w>>20,12),writes=rd)
    elif op==3:
        d.update(kind='load' if f==2 else 'other',rd=rd,rs=rs,imm=signed(w>>20,12),writes=rd)
    elif op==0x23:
        d.update(kind='store' if f==2 else 'store_other',rs=rs,value_rs=(w>>20)&31,imm=signed(((w>>25)<<5)|((w>>7)&31),12))
    elif op in (0x13,0x33,0x2f,0x73):
        d.update(writes=rd)
    return d


class Audit:
    def __init__(self,elf,indirect=None,boundaries=None):
        self.elf=elf;self.indirect=indirect or {};self.boundaries=boundaries or {}
        self.code={};self.data={};self.edges=[];self.dynamic=[];self.exclusions=[]

    def safe(self,address,size=1,code=False):
        start,end=IRAM if code else DRAM
        if start<=address and address+max(size,1)<=end:
            return
        if code and any(address==value for value in ROM.values()):
            return
        raise ValueError(('Flash callback/code or unsafe code: ' if code else 'Flash/unsafe data: ')+hex(address))

    def code_target(self,address):
        self.safe(address,code=True)
        if address in ROM.values():
            name=next(name for name,value in ROM.items() if value==address)
            symbol=self.elf.symbol(name)
            if symbol['address']!=address or symbol['section']!=0xfff1:
                raise ValueError('ROM target is not pinned ABS symbol: '+name)
            self.code[address]=dict(name=name,address=address,bytes=0,rom=True)
            return
        symbol=self.elf.containing(address,2)
        if not symbol:
            raise ValueError('code target has no sized function: '+hex(address))
        self.function(symbol)

    def data_target(self,address):
        self.safe(address)
        symbol=self.elf.containing(address,1)
        start=symbol['address'] if symbol else address;size=symbol['size'] if symbol else 4
        self.safe(start,size)
        if start in self.data:
            return
        self.data[start]=dict(name=symbol['name'] if symbol else '<interior>',address=start,bytes=size)
        for rel in self.elf.relocs:
            if start<=rel['address']<start+size:
                self.reference(rel)

    def reference(self,rel):
        typ,symbol=rel['type'],rel['symbol']
        if typ in (0,43,51): # NONE, ALIGN and RELAX carry no address
            return
        if typ in (24,25): # PCREL_LO12 names the associated HI20 location
            matches=[r for r in self.elf.relocs if r['address']==symbol['address']+rel['addend'] and r['type'] in (20,23)]
            if len(matches)!=1:
                raise ValueError('unresolved PCREL low/high relocation')
            high=matches[0];target=high['symbol']['address']+high['addend']
            low_ins=decode(self.elf.read(rel['address'],4),rel['address'])
            high_ins=decode(self.elf.read(high['address'],4),high['address'])
            if low_ins.get('rs')!=high_ins.get('rd') or low_ins.get('imm')!=signed((target-high['address'])&0xfff,12):
                raise ValueError('PCREL low relocation differs from final instruction')
            self.reference(matches[0]);return
        if typ not in (1,2,16,17,18,19,20,23,26,27,28,33,34,35,36,37,38,39,40,44,45,52,53,54,55,56,57):
            raise ValueError('unexplained allocated relocation type '+str(typ))
        if not symbol['section']:
            raise ValueError('undefined allocated relocation: '+symbol['name'])
        target=(symbol['address']+rel['addend'])&0xffffffff
        if typ in (1,2):
            width=4 if typ==1 else 8
            if int.from_bytes(self.elf.read(rel['address'],width),'little')!=target:
                raise ValueError('pointer relocation differs from final data at '+hex(rel['address']))
        elif typ in (26,27,28):
            ins=decode(self.elf.read(rel['address'],4),rel['address'])
            if typ==26:
                if ins['kind']!='constant' or ins.get('value')!=((target+0x800)&0xfffff000):
                    raise ValueError('HI20 relocation differs from final instruction')
            elif ins.get('imm')!=signed(target&0xfff,12):
                raise ValueError('LO12 relocation differs from final instruction')
        elif typ==23:
            ins=decode(self.elf.read(rel['address'],4),rel['address'])
            expected=(rel['address']+((target-rel['address']+0x800)&0xfffff000))&0xffffffff
            if ins['kind']!='constant' or ins.get('value')!=expected:
                raise ValueError('PCREL HI20 relocation differs from final instruction')
        section=self.elf.sections[symbol['section']] if symbol['section']<len(self.elf.sections) else None
        if symbol['type']==2 or (section and section['flags']&4) or target in ROM.values():
            self.code_target(target)
        else:
            self.data_target(target)

    def function(self,symbol):
        start,size=symbol['address'],symbol['size'];self.safe(start,size,code=True)
        if start in self.code:
            return
        if not size:
            raise ValueError('zero-sized application function: '+symbol['name'])
        self.code[start]=dict(name=symbol['name'],address=start,bytes=size,sha256=sha(self.elf.read(start,size)))
        if symbol['name'] in self.boundaries:
            self.exclusions.append(dict(function=symbol['name'],reason=self.boundaries[symbol['name']]))
            return
        own=[r for r in self.elf.relocs if start<=r['address']<start+size]
        instructions={};pc=start
        while pc<start+size:
            ins=decode(self.elf.read(pc,min(4,start+size-pc)),pc)
            if pc+ins['size']>start+size:raise ValueError('function ends inside instruction')
            instructions[pc]=ins;pc+=ins['size']
        # Forward dataflow with conservative joins. A provenance fact survives
        # a branch/loop only when every incoming path agrees; no linear-scan
        # guess can authorize an indirect call from a stale register value.
        states={start:{0:('constant',0),2:('stack',0),**{10+i:('argument',i) for i in range(8)}}};pending=[start]
        def transfer(registers,ins):
            registers=dict(registers);kind=ins['kind']
            if kind=='constant':registers[ins['rd']]=('constant',ins['value'])
            elif kind=='move':registers[ins['rd']]=registers.get(ins['rs'])
            elif kind=='addi':
                base=registers.get(ins['rs'])
                if base and base[0]=='constant':value=('constant',(base[1]+ins['imm'])&0xffffffff)
                elif base and base[0]=='stack':value=('stack',base[1]+ins['imm'])
                elif not ins['imm']:value=base
                elif base and len(repr(base))<120:value=('offset',base,ins['imm'])
                else:value=None
                registers[ins['rd']]=value
            elif kind=='load':
                base=registers.get(ins['rs']);offset=ins['imm']
                while base and base[0]=='offset':offset+=base[2];base=base[1]
                if base and base[0]=='constant':base=('constant',(base[1]+offset)&0xffffffff);offset=0
                if base and base[0]=='stack':value=registers.get(('stackword',base[1]+offset))
                else:value=('load',base,offset) if base and len(repr(base))<160 else None
                registers[ins['rd']]=value
            elif kind=='store':
                base=registers.get(ins['rs'])
                if base and base[0]=='stack':registers[('stackword',base[1]+ins['imm'])]=registers.get(ins['value_rs'])
                else:
                    for key in list(registers):
                        if isinstance(key,tuple):registers.pop(key,None)
            elif kind=='store_other':
                for key in list(registers):
                    if isinstance(key,tuple):registers.pop(key,None)
            elif ins.get('writes') is not None:registers.pop(ins['writes'],None)
            if kind in ('direct','indirect') and ins.get('rd',0):
                # ABI-preserved caller stack slots are usable only while their
                # address has not escaped through a call argument. Unknown
                # callees never supply a guessed return value or stack write.
                if any(registers.get(reg,('',))[0]=='stack' for reg in range(10,18)):
                    for key in list(registers):
                        if isinstance(key,tuple):registers.pop(key,None)
                for reg in (1,5,6,7,10,11,12,13,14,15,16,17,28,29,30,31):registers.pop(reg,None)
            registers[0]=('constant',0);return {k:v for k,v in registers.items() if v is not None}
        while pending:
            at=pending.pop();ins=instructions[at];kind=ins['kind'];after=transfer(states[at],ins);successors=[]
            following=at+ins['size']
            if kind=='branch':successors=[following,ins['target']]
            elif kind=='direct' and not ins.get('rd'):successors=[ins['target']]
            elif kind!='return' and not (kind=='indirect' and not ins.get('rd')):successors=[following]
            for target in successors:
                if not start<=target<start+size:continue
                if target not in instructions:raise ValueError('control transfer inside instruction')
                merged=after if target not in states else {k:v for k,v in states[target].items() if after.get(k)==v}
                if target not in states or merged!=states[target]:states[target]=merged;pending.append(target)
        controls={};targets=[]
        pc=start
        while pc<start+size:
            ins=instructions[pc];kind=ins['kind'];registers=states.get(pc,{})
            if kind in ('direct','branch','indirect'):
                target=ins.get('target');expr=None
                if kind=='indirect':
                    expr=registers.get(ins['rs'])
                    if expr and expr[0]=='constant':target=(expr[1]+ins['imm'])&0xfffffffe
                    else:
                        key=(symbol['name'],repr(expr))
                        if key not in self.indirect:
                            raise ValueError('unexplained indirect callsite '+symbol['name']+'+'+hex(pc-start)+' '+repr(expr))
                        target,reason=self.indirect[key]
                        self.dynamic.append(dict(owner=symbol['name'],pc=pc,expression=repr(expr),target=target,reason=reason))
                if target is not None:
                    controls[pc]=target
                    if not start<=target<start+size:
                        self.edges.append(dict(owner=symbol['name'],pc=pc,target=target));targets.append(target)
            pc+=ins['size']
        for rel in own:
            if rel['type'] in (16,17,18,19,44,45):
                target=(rel['symbol']['address']+rel['addend'])&0xffffffff
                # CALL reloc may remain on relaxed JAL/C.J, or on AUIPC
                # whose following JALR still carries the actual control edge.
                actual=controls.get(rel['address'],controls.get(rel['address']+4))
                if actual!=target:
                    raise ValueError('relocation differs from final instruction at '+hex(rel['address']))
            self.reference(rel)
        for target in targets:self.code_target(target)

    def closure(self,roots):
        for name in roots:self.code_target(self.elf.symbol(name)['address'])
        return dict(code=sorted(self.code.values(),key=lambda x:x['address']),
                    data=sorted(self.data.values(),key=lambda x:x['address']),edges=self.edges,
                    indirect=self.dynamic,boundaries=self.exclusions)


def baseline_rejections(tree):
    tree=Path(tree);elf=ELF(regular(tree/'build/xiaozhi.elf'));failures=[]
    if 'CONFIG_I2S_ISR_IRAM_SAFE=y' not in regular(tree/'sdkconfig').decode().splitlines():failures.append('IRAM config off')
    for name in CALLBACKS:
        if not IRAM[0]<=elf.symbol(name)['address']<IRAM[1]:failures.append('Flash callback: '+name)
    for name in (DISABLE,ENABLE):
        try:elf.symbol('__wrap_'+name)
        except ValueError:failures.append('missing wrapper: '+name)
    if not elf.relocs:failures.append('missing emitted relocations')
    return failures


def source_pins():
    return json.loads(regular(Path(__file__).with_name('family_c3_sdk_pins.json')))


def capture(tree,idf,link_command):
    """Build-side input retention only; this function is never needed to inspect."""
    tree=Path(tree);out=tree/'build/family_c3_cache';out.mkdir(exist_ok=True)
    for name,expected in source_pins().items():
        data=regular(Path(idf)/name)
        if sha(data)!=expected:raise ValueError('fixed SDK source drift: '+name)
        target=out/'sdk'/name;target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(data)
    (out/'link-command.txt').write_text(link_command)


def capture_build(tree,idf):
    # -x expands rspfile CONTENT from Ninja's actual graph, even after Ninja
    # has deleted the temporary response file following a successful link.
    result=subprocess.run(['ninja','-C',str(Path(tree)/'build'),'-t','compdb','-x'],capture_output=True,text=True,check=True)
    capture(tree,idf,select_link_command(result.stdout))


def select_link_command(compdb):
    commands=[entry['command'] for entry in json.loads(compdb)
              if Path(entry.get('output','')).name=='xiaozhi.elf' or re.search(r'(?:^|\s)-o\s+xiaozhi\.elf(?:\s|$)',entry['command'])]
    if len(commands)!=1:raise ValueError('unable to retain unique actual native linker command')
    if any(token.startswith(('@','-Wl,@')) for token in shlex.split(commands[0])):
        raise ValueError('actual native linker response file was not expanded')
    return commands[0]+'\n'


def verify_report(tree):
    report=audit_tree(tree)
    encoded=json.dumps(report,indent=2,sort_keys=True)+'\n'
    if regular(Path(tree)/'build/family_c3_cache/iram-audit.json').decode()!=encoded:
        raise ValueError('retained IRAM audit report differs from independent recomputation')
    return report


def audit_tree(tree):
    tree=Path(tree);build=tree/'build';inputs=build/'family_c3_cache'
    failures=baseline_rejections(tree)
    if failures:raise ValueError('; '.join(failures))
    elf_bytes=regular(build/'xiaozhi.elf');elf=ELF(elf_bytes)
    config=regular(tree/'sdkconfig').decode();mapping=regular(build/'xiaozhi.map')
    for option in ('CONFIG_IDF_TARGET="esp32c3"','CONFIG_XMINI_C3_PCM=y','CONFIG_GDMA_ISR_HANDLER_IN_IRAM=y',
                   'CONFIG_ESP_TIMER_IN_IRAM=y','CONFIG_FREERTOS_UNICORE=y','# CONFIG_SPI_FLASH_AUTO_SUSPEND is not set'):
        if option not in config.splitlines():raise ValueError('cache-safe SDK configuration missing: '+option)
    sources={name:sha(regular(tree/name)) for name in APPLICATION_INPUTS}
    compiled=json.loads(regular(build/'compile_commands.json'))
    invocations={}
    for name in ('family_c3_isr.cc','family_c3_sdk_layout.c'):
        entries=[entry for entry in compiled if Path(entry['file']).name==name]
        if len(entries)!=1:raise ValueError('native ISR/layout must each compile exactly once: '+name)
        entry=entries[0];args=entry.get('arguments') or shlex.split(entry['command'])
        if '-fno-weak' in args or '-Wno-error' in args:raise ValueError('unsafe ISR compiler warning/COMDAT workaround')
        if name.endswith('.cc') and '-fno-lto' not in args:raise ValueError('dedicated ISR object requires non-LTO placement')
        invocations[name]=args
    pins=source_pins()
    for name,expected in pins.items():
        if sha(regular(inputs/'sdk'/name))!=expected:raise ValueError('retained fixed SDK source drift: '+name)
    link=regular(inputs/'link-command.txt').decode()
    for option in ('--emit-relocs','--wrap='+DISABLE,'--wrap='+ENABLE):
        if option not in link:raise ValueError('actual linker arguments missing '+option)
    if b'family_c3_isr.cc.obj' not in mapping:raise ValueError('ISR object absent from actual map')
    abi=elf.symbol('family_c3_sdk_layout');layout=struct.unpack('<13I',elf.read(abi['address'],52))
    if layout[:2]!=(1,4) or abi['size']!=52:raise ValueError('invalid native SDK ABI evidence')
    indirect={}
    # Only these exact field loads are covered by reviewed registrations.
    for owner,base,fields in (
        ('i2s_dma_rx_callback',('argument',2),[(layout[2],CALLBACKS[1]),(layout[3],None)]),
        ('i2s_dma_tx_callback',('argument',2),[(layout[4],CALLBACKS[0]),(layout[5],None)]),
        ('gdma_default_rx_isr',('argument',0),[(layout[8],'i2s_dma_rx_callback'),(layout[9],None),(layout[10],None),(layout[11],None)]),
        ('gdma_default_tx_isr',('argument',0),[(layout[6],'i2s_dma_tx_callback'),(layout[7],None)])):
        for offset,target in fields:
            expr=('load',base,offset)
            # Null callbacks are source-proven unreachable, represented by an
            # explicit exclusion rather than inventing a runnable target.
            indirect[(owner,repr(expr))]=(elf.symbol(target)['address'] if target else None,
                'fixed SDK callback field + native offsetof; '+('registered '+target if target else 'zero-initialized, never registered'))
    timer=elf.symbol('systimer_hal')
    for owner in ('esp_timer_impl_get_time','esp_timer_get_time'):
        indirect[(owner,repr(('load',('constant',timer['address']+layout[12]),0)))]=(elf.symbol('systimer_ticks_to_us')['address'],
            'fixed timer init installs systimer_ticks_to_us; native offsetof(ticks_to_us)')
    boundaries={DISABLE:'fixed SDK cache guard owns interrupt/cache transition; original invocation and IRAM placement separately verified',
                ENABLE:'fixed SDK cache guard restores scheduler; enable_after intentionally follows its return'}
    audit=Audit(elf,indirect,boundaries)
    roots=[*CALLBACKS,'__wrap_'+DISABLE,'__wrap_'+ENABLE,'gdma_default_rx_isr','gdma_default_tx_isr']
    closure=audit.closure(roots)
    # Prove wrapper interposition on the real flash OS path, not flag strings.
    for owner,target in [('spi1_start','__wrap_'+DISABLE),('spi1_end','__wrap_'+ENABLE),
                         ('__wrap_'+DISABLE,DISABLE),('__wrap_'+ENABLE,ENABLE)]:
        # spi1 methods span cache-on and cache-off phases; inspect only their
        # decoded direct edges, not their cache-on-only setup callees.
        symbol=elf.symbol(owner);at=symbol['address'];end=at+symbol['size'];edges=[];previous=None
        while at<end:
            ins=decode(elf.read(at,min(4,end-at)),at)
            if ins['kind']=='direct':edges.append(ins['target'])
            if ins['kind']=='indirect' and previous and previous[1]['kind']=='constant' and previous[1].get('rd')==ins['rs']:
                edges.append((previous[1]['value']+ins['imm'])&0xfffffffe)
            previous=(at,ins);at+=ins['size']
        if elf.symbol(target)['address'] not in edges:raise ValueError('actual flash guard route missing: '+owner+' -> '+target)
    app=regular(build/'xiaozhi.bin')
    if len(app)<208 or app[176:208]!=hashlib.sha256(elf_bytes).digest():raise ValueError('audit ELF differs from actual app descriptor')
    return dict(schema=1,sdk_commit=SDK_COMMIT,elf_sha256=sha(elf_bytes),app_sha256=sha(app),map_sha256=sha(mapping),
                sdkconfig_sha256=sha(config.encode()),link_command_sha256=sha(link.encode()),sdk_source_sha256=pins,
                application_inputs_sha256=sources,native_invocations=invocations,
                native_layout=layout,closure=closure,observer_bytes_budget=1024,
                guarantees='cache-off IRQ code/data placement; conservative observed spans only; no acoustic or physical DMA claim')


def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--tree',required=True,type=Path)
    parser.add_argument('--capture-idf',type=Path);parser.add_argument('--check',action='store_true')
    args=parser.parse_args();out=args.tree/'build/family_c3_cache'
    if args.capture_idf:
        capture_build(args.tree,args.capture_idf)
    report=audit_tree(args.tree);encoded=json.dumps(report,indent=2,sort_keys=True)+'\n'
    target=out/'iram-audit.json'
    if args.check:
        if regular(target).decode()!=encoded:raise ValueError('retained IRAM audit report differs from independent recomputation')
    else:target.write_text(encoded)
    print('C3 final ELF cache-safe audit passed: '+report['elf_sha256'])


if __name__=='__main__':main()
