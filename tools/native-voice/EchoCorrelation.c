#include "EchoCorrelation.h"
#include <math.h>
#include <stdlib.h>
#include <string.h>
#include <limits.h>

/* Single analysis worker owns this object. No callbacks, files, or audio APIs. */
enum { RATE=4000, WINDOW=1000, REF_CAP=12000, MIC_CAP=24000,
       MIN_LAG=-800, MAX_LAG=3200, STEP=1, CONTEXTS=2 };
typedef struct {
    int active, ended, next, length, anchors;
    int group_count;
    int64_t group_frame, next_frame;
    double group_sum;
    int64_t id, generation;
    uint64_t sequence;
    double origin, origin_min, origin_max;
    double converter_offset_min, converter_offset_max;
    double mapping_offset_min, mapping_offset_max;
    double source_clock_min, source_clock_max;
    float reference[REF_CAP];
    unsigned char valid[REF_CAP];
} EchoPlayback;
struct FCEchoProbe {
    EchoPlayback playbacks[CONTEXTS];
    float microphone[MIC_CAP];
    int64_t mic_ticks[MIC_CAP], latest;
    double previous_capture_end;
    FCEchoCaptureStats capture_stats;
    double source_jump_s;
    double capture_group_sum, capture_group_host;
    int capture_group_count;
    int capture_clock_bad, closed;
    uint64_t sequence, dropped;
};
static int valid_time(double t) { return isfinite(t) && t>0 && t<1e10; }
static int64_t tick(double t) { return (int64_t)llround(t*RATE); }
static size_t slot(int64_t t) { return (size_t)((uint64_t)t % MIC_CAP); }
static EchoPlayback *find(FCEchoProbe *p,int64_t g) {
    if (!p) return NULL;
    for(int i=0;i<CONTEXTS;i++) if(p->playbacks[i].active && p->playbacks[i].generation==g) return &p->playbacks[i];
    return NULL;
}
static void wipe(void *address,size_t length) {
    volatile unsigned char *p=address;
    while(length--) *p++=0;
}
FCEchoProbe *fc_echo_create(void) {
    FCEchoProbe *p=calloc(1,sizeof(*p));
    if(p) {
        p->latest=INT64_MIN;
        for(int i=0;i<MIC_CAP;i++) p->mic_ticks[i]=INT64_MIN;
    }
    return p;
}
void fc_echo_destroy(FCEchoProbe *p) { if(p) { wipe(p,sizeof(*p)); free(p); } }
void fc_echo_begin(FCEchoProbe *p,int64_t id,int64_t g) {
    if(!p || p->closed || id<=0 || g<=0 || find(p,g)) return;
    EchoPlayback *c=NULL;
    for(int i=0;i<CONTEXTS;i++) if(!p->playbacks[i].active) { c=&p->playbacks[i]; break; }
    if(!c) {
        c=&p->playbacks[0];
        for(int i=1;i<CONTEXTS;i++) if(p->playbacks[i].sequence<c->sequence) c=&p->playbacks[i];
        p->dropped++;
    }
    memset(c,0,sizeof(*c)); c->active=1; c->id=id; c->generation=g; c->sequence=++p->sequence;
    c->converter_offset_min=c->converter_offset_max=p->capture_stats.converter_offset_ms;
    c->mapping_offset_min=c->mapping_offset_max=p->capture_stats.source_pcm_offset_ms;
    c->source_clock_min=c->source_clock_max=p->capture_stats.source_clock_error_ms;
}
void fc_echo_reference(FCEchoProbe *p,int64_t g,const int16_t *pcm,size_t count,int64_t frame) {
    EchoPlayback *c=find(p,g);
    if(!c || c->ended || !pcm || !count || count>4096 || frame<0 || frame>=72000) return;
    if(frame!=c->next_frame) { c->group_count=0; c->group_sum=0; }
    c->next_frame=frame+(int64_t)count;
    for(size_t i=0;i<count && frame+(int64_t)i<72000;i++) {
        if(!c->group_count) c->group_frame=frame+(int64_t)i;
        c->group_sum+=pcm[i]/32768.0; c->group_count++;
        if(c->group_count==6) {
            int index=(int)llround(c->group_frame/6.0);
            if(index<REF_CAP) {
                c->reference[index]=(float)(c->group_sum/6); c->valid[index]=1;
                if(index>=c->length) c->length=index+1;
            }
            c->group_count=0; c->group_sum=0;
        }
    }
}
void fc_echo_anchor(FCEchoProbe *p,int64_t g,double origin) {
    EchoPlayback *c=find(p,g);
    if(!c || !valid_time(origin)) return;
    if(c->anchors==0) c->origin=c->origin_min=c->origin_max=origin;
    c->origin_min=fmin(c->origin_min,origin); c->origin_max=fmax(c->origin_max,origin);
    c->anchors++;
}
void fc_echo_capture_source(FCEchoProbe *p,const int16_t *pcm,size_t count,double host,size_t source_frames,double source_rate) {
    if(!p || p->closed || !pcm || !count || count>4096 || !valid_time(host) || !source_frames || source_frames>131072
       || !isfinite(source_rate) || source_rate<8000 || source_rate>384000) return;
    FCEchoCaptureStats *s=&p->capture_stats;
    double delta=s->blocks?host-s->last_source_end_s:0;
    double assigned=s->blocks?p->previous_capture_end:host;
    /* The source timestamp belongs to SOURCE frames, not this call's SRC
       output. Priming/buffering can legitimately change output block size. */
    if(s->blocks && fabs(delta)>1.0/RATE) {
        assigned+=delta;
        p->source_jump_s+=delta;
        if(delta<0) { p->capture_clock_bad=1; s->source_backwards++; }
        else s->source_gaps++;
        p->capture_group_count=0; p->capture_group_sum=0;
    }
    if(!s->blocks) s->first_source_host_s=host;
    s->blocks++;s->source_frames+=source_frames;s->converted_samples+=count;
    s->source_duration_s+=source_frames/source_rate;s->pcm_duration_s+=count/16000.0;
    s->last_source_end_s=host+source_frames/source_rate;
    s->source_delta_ms=delta*1000;
    s->source_delta_min_ms=fmin(s->source_delta_min_ms,s->source_delta_ms);
    s->source_delta_max_ms=fmax(s->source_delta_max_ms,s->source_delta_ms);
    s->converter_offset_ms=(s->pcm_duration_s-s->source_duration_s)*1000;
    s->source_pcm_offset_ms=(host-assigned)*1000;
    /* Source-clock rate error is independent of how SRC batches deliveries.
       Recognized discontinuities retain holes instead of masquerading as drift. */
    s->source_clock_error_ms=((s->last_source_end_s-s->first_source_host_s)-s->source_duration_s-p->source_jump_s)*1000;
    for(int i=0;i<CONTEXTS;i++) if(p->playbacks[i].active) {
        EchoPlayback *c=&p->playbacks[i];
        c->converter_offset_min=fmin(c->converter_offset_min,s->converter_offset_ms);
        c->converter_offset_max=fmax(c->converter_offset_max,s->converter_offset_ms);
        c->mapping_offset_min=fmin(c->mapping_offset_min,s->source_pcm_offset_ms);
        c->mapping_offset_max=fmax(c->mapping_offset_max,s->source_pcm_offset_ms);
        c->source_clock_min=fmin(c->source_clock_min,s->source_clock_error_ms);
        c->source_clock_max=fmax(c->source_clock_max,s->source_clock_error_ms);
    }
    p->previous_capture_end=assigned+count/16000.0;
    for(size_t i=0;i<count;i++) {
        if(!p->capture_group_count) p->capture_group_host=assigned+i/16000.0;
        p->capture_group_sum+=pcm[i]/32768.0; p->capture_group_count++;
        if(p->capture_group_count==4) {
            int64_t t=tick(p->capture_group_host+1.5/16000.0);
            p->microphone[slot(t)]=(float)(p->capture_group_sum/4); p->mic_ticks[slot(t)]=t;
            if(t>p->latest) p->latest=t;
            p->capture_group_count=0; p->capture_group_sum=0;
        }
    }
}
void fc_echo_capture(FCEchoProbe *p,const int16_t *pcm,size_t count,double host) {
    fc_echo_capture_source(p,pcm,count,host,count,16000);
}
void fc_echo_capture_stats(const FCEchoProbe *p,FCEchoCaptureStats *out) { if(out) { if(p) *out=p->capture_stats; else memset(out,0,sizeof(*out)); } }
void fc_echo_end(FCEchoProbe *p,int64_t g) { EchoPlayback *c=find(p,g); if(c) c->ended=1; }
void fc_echo_close(FCEchoProbe *p) {
    if(!p) return;
    p->closed=1;
    for(int i=0;i<CONTEXTS;i++) if(p->playbacks[i].active) p->playbacks[i].ended=1;
}
typedef struct { double correlation, yy, xy; } Candidate;
/* 0: gap; 1: usable; 2: complete but below the observation energy floor. */
static int candidate(const FCEchoProbe *p,const float *x,int n,int64_t start,int lag,
                     double sx,double xx,Candidate *out) {
    double sy=0,syy=0,sxy=0;
    for(int i=0;i<n;i++) {
        int64_t t=start+i+lag; size_t at=slot(t);
        if(p->mic_ticks[at]!=t) return 0;
        double y=p->microphone[at]; sy+=y; syy+=y*y; sxy+=x[i]*y;
    }
    double yy=fmax(0,syy-sy*sy/n),xy=sxy-sx*sy/n;
    if(yy/n<1e-10) return 2;
    out->yy=yy; out->xy=xy;
    out->correlation=fmax(-1,fmin(1,xy/sqrt(xx*yy)));
    return 1;
}
static void measure(const FCEchoProbe *p,const EchoPlayback *c,FCEchoResult *r,int64_t start) {
    const float *x=c->reference+c->next;
    /* A hole must not silently narrow the lag search to a misleading peak. */
    for(int64_t t=start+MIN_LAG;t<start+r->samples+MAX_LAG;t++)
        if(p->mic_ticks[slot(t)]!=t) { r->status=FC_ECHO_CAPTURE_GAP; return; }
    double sx=0,sxx=0;
    for(int i=0;i<r->samples;i++) {
        if(!c->valid[c->next+i]) { r->status=FC_ECHO_REFERENCE_GAP; return; }
        sx+=x[i]; sxx+=x[i]*x[i];
    }
    double xx=fmax(0,sxx-sx*sx/r->samples);
    r->reference_rms=sqrt(xx/r->samples);
    if(xx/r->samples<1e-10) { r->status=FC_ECHO_LOW_REFERENCE; return; }
    double peaks[(MAX_LAG-MIN_LAG)/STEP+1];
    int best_lag=0,complete=0,found=0; Candidate best={0,0,0};
    for(int lag=MIN_LAG;lag<=MAX_LAG;lag+=STEP) {
        Candidate value={0,0,0}; int valid=candidate(p,x,r->samples,start,lag,sx,xx,&value);
        peaks[(lag-MIN_LAG)/STEP]=valid==1?fabs(value.correlation):-1;
        if(valid) complete=1;
        if(valid==1) {
            r->tested_lags++;
            if(!found || fabs(value.correlation)>fabs(best.correlation)) { found=1; best=value; best_lag=lag; }
        }
    }
    if(!found) { r->status=complete?FC_ECHO_LOW_CAPTURE:FC_ECHO_CAPTURE_GAP; return; }
    for(int lag=MIN_LAG;lag<=MAX_LAG;lag+=STEP)
        if(abs(lag-best_lag)>=80) r->second_peak=fmax(r->second_peak,peaks[(lag-MIN_LAG)/STEP]);
    r->correlation=best.correlation; r->lag_ms=best_lag*1000.0/RATE;
    r->capture_rms=sqrt(best.yy/r->samples); r->projection_gain=best.xy/xx;
    r->projected_capture_rms=fabs(r->projection_gain)*r->reference_rms;
    r->boundary=best_lag<=MIN_LAG+STEP || best_lag>=MAX_LAG-STEP;
}
int fc_echo_poll(FCEchoProbe *p,FCEchoResult *r) {
    if(!p || !r) return 0;
    for(int pass=0;pass<CONTEXTS;pass++) {
        EchoPlayback *c=&p->playbacks[pass];
        if(!c->active) continue;
        if(c->next>=c->length && (c->ended || c->next>=REF_CAP)) { c->active=0; continue; }
        int n=c->length-c->next;
        if(n>WINDOW) n=WINDOW;
        if(n<WINDOW && !c->ended) continue;
        /* An end callback can precede admission of its immutable clock snapshot. */
        if(c->anchors==0 && !p->closed) continue;
        int64_t start=c->anchors?tick(c->origin+(c->next*6+2.5)/24000.0):0;
        int ready=c->anchors && p->latest>=start+n-1+MAX_LAG;
        double converter_spread=c->converter_offset_max-c->converter_offset_min;
        double mapping_spread=c->mapping_offset_max-c->mapping_offset_min;
        double source_spread=c->source_clock_max-c->source_clock_min;
        if(!ready && !p->closed && c->anchors>0 && c->origin_max-c->origin_min<=0.010 && !p->capture_clock_bad && !p->capture_stats.source_gaps && source_spread<=2) continue;
        memset(r,0,sizeof(*r)); r->playback_id=c->id; r->generation=c->generation;
        r->samples=n; r->window_start_ms=c->next*1000/RATE; r->anchors=c->anchors;
        r->reference_origin_host_s=c->origin; r->window_host_s=c->anchors?(double)start/RATE:0;
        r->anchor_spread_ms=(c->origin_max-c->origin_min)*1000;
        r->converter_offset_spread_ms=converter_spread;
        r->capture_mapping_spread_ms=mapping_spread;
        r->source_clock_spread_ms=source_spread;
        if(c->anchors==0) r->status=FC_ECHO_CLOCK_MISSING;
        else if(r->anchor_spread_ms>10 || p->capture_clock_bad || source_spread>2) r->status=FC_ECHO_CLOCK_UNSTABLE;
        /* A lost SRC delivery's output length is unknown. Do not repair its
           sample clock using only source duration, even for later windows. */
        else if(p->capture_stats.source_gaps) r->status=FC_ECHO_CAPTURE_GAP;
        else if(n<400) r->status=FC_ECHO_SHORT_WINDOW;
        else if(!ready) r->status=FC_ECHO_INCOMPLETE;
        else measure(p,c,r,start);
        c->next+=n>0?n:WINDOW;
        if(c->next>=REF_CAP || (c->ended && c->next>=c->length)) {
            c->active=0; wipe(c->reference,sizeof(c->reference)); memset(c->valid,0,sizeof(c->valid));
        }
        return 1;
    }
    return 0;
}
uint64_t fc_echo_dropped_playbacks(const FCEchoProbe *p) { return p?p->dropped:0; }
