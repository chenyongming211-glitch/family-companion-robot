"""Cloud-only digital-supply and raw MR statistics, not an acoustic pass score."""
import argparse
import hashlib
import json
import pathlib
import re
import numpy as np

def channel_stats(values):
    x=np.asarray(values,dtype=np.float64)
    if len(x)<24000:return {'available':False,'reason':'less_than_one_second'}
    # Exclude onset and deliberate end. Four 400Hz periods per 10ms window make
    # DC/sin/cos orthogonal. Residuals detect departures, not their physical cause.
    x=x[12000:-12000] if len(x)>48000 else x
    x=x[:len(x)//240*240].reshape(-1,240)
    phase=2*np.pi*400*np.arange(240)/24000
    mean=x.mean(axis=1);s=x@np.sin(phase)*2/240;c=x@np.cos(phase)*2/240
    residual=x-mean[:,None]-s[:,None]*np.sin(phase)-c[:,None]*np.cos(phase)
    rms=np.sqrt(np.mean(residual*residual,axis=1));med=float(np.median(rms));mad=float(np.median(np.abs(rms-med)))
    threshold=max(30.,med*4,med+12*1.4826*mad)
    ordinary=np.sqrt(np.mean(x*x,axis=1));ordinary_med=float(np.median(ordinary))
    return {'available':True,'windows':len(x),'rms':float(np.sqrt(np.mean(x*x))),'peak':float(np.max(np.abs(x))),'clipped_samples':int(np.count_nonzero(np.abs(x)>=32760)),'tone_amplitude_median':float(np.median(np.sqrt(s*s+c*c))),'residual_rms_median':med,'residual_rms_max':float(rms.max()),'residual_peak':float(np.abs(residual).max()),'large_residual_windows':int(np.count_nonzero(rms>threshold)),'residual_threshold':threshold,'near_silence_windows':int(np.count_nonzero(ordinary<ordinary_med*.05)) if ordinary_med>1 else len(x),'max_adjacent_step':float(np.max(np.abs(np.diff(x.reshape(-1)))))}

def analyze(root,descriptors):
    root=pathlib.Path(root);summary=json.loads((root/'summary.json').read_text())
    if not summary['complete']:raise ValueError('incomplete_capture')
    times={};results={}
    for text in (root/'metadata.jsonl').read_text().splitlines():
        d=json.loads(text);line=d.get('line','')
        m=re.fullmatch(r'WPL_INPUT_TIME ([0-7]) (\d+) (\d+)',line)
        if m:times.setdefault(m[1],[]).append((int(m[2]),int(m[3])))
        if d.get('kind')=='result':
            p=line.split()[1];results[p]={k:int(v) for k,v in re.findall(r'([a-z_]+)=(-?\d+)',line)}
    phases={}
    for p,info in summary['captures'].items():
        path=root/f'phase-{p}.pcm';raw=path.read_bytes()
        if hashlib.sha256(raw).hexdigest()!=info['sha256']:raise ValueError('raw_digest_changed')
        pcm=np.frombuffer(raw,dtype='<i2').reshape(-1,2)
        ts=times.get(p,[])
        if ts and (ts[0][0]!=0 or any(b[0]<=a[0] or b[1]<=a[1] for a,b in zip(ts,ts[1:]))):raise ValueError('capture_timestamp_order')
        gaps=np.array([(b[1]-a[1])/1000 for a,b in zip(ts,ts[1:])])
        supply=results[p]
        span=(ts[-1][1]-ts[0][1])/1e6 if len(ts)>1 else 0
        delivered_rate=(ts[-1][0]-ts[0][0])/2/span if span else None
        phases[p]={'supply':supply,'all_expected_samples_completed':supply.get('completed_samples')==288000,'empty_fraction':supply.get('empty',0)/max(1,supply.get('dma',0)),'capture_seconds':len(pcm)/24000,'input_read_max_gap_ms':float(gaps.max()) if len(gaps) else None,'input_read_gaps_over_rx_capacity':int(np.count_nonzero(gaps>descriptors*10)) if len(gaps) else None,'input_observed_frames_per_s':delivered_rate,'input_timing_span_s':span,'input_timing_records':len(ts),'ch0':channel_stats(pcm[:,0]),'ch1':channel_stats(pcm[:,1]),'continuity_note':'ADC reads have no hardware sequence stamp; read backlog can mimic a waveform discontinuity','phase0_count_note':'DMA counter includes preexisting untagged startup samples; do not interpret its excess as duplicate lab PCM' if p=='0' else None}
    return {'schema':1,'dma_descriptors':descriptors,'phases':phases,'phase_names':{'0':'direct_pcm','1':'buffered_opus_pri2','2':'buffered_opus_pri6','3':'buffered_opus_screen_off','4':'local_60ms_paced_opus','5':'network_burst_opus','6':'network_60ms_paced_opus','7':'direct_pcm_amplitude_12000'},'claim':'diagnostic measurements; no automatic causal or acoustic pass verdict'}

if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--input',required=True);p.add_argument('--descriptors',required=True,type=int,choices=(6,12));p.add_argument('--output',required=True);a=p.parse_args()
    result=analyze(a.input,a.descriptors)
    with pathlib.Path(a.output).open('x') as out:json.dump(result,out,indent=2);out.write('\n')
    for phase,d in result['phases'].items():print(json.dumps({'phase':phase,'supply':d['supply'],'input_read_max_gap_ms':d['input_read_max_gap_ms'],'ch0_large_residual_windows':d['ch0'].get('large_residual_windows'),'ch1_large_residual_windows':d['ch1'].get('large_residual_windows')}))
