#include "../EchoCorrelation.h"
#include <assert.h>
#include <math.h>
#include <stdio.h>
#include <stdlib.h>

/* A deterministic band-limited source, not a recording. */
static double source(double t) {
    if (t < 0 || t >= 3) return 0;
    const double pi = 3.141592653589793;
    return 0.24 * sin(2*pi*(137*t+117*t*t)) + 0.17*sin(2*pi*(311*t+47*t*t))
        + 0.11*sin(2*pi*(743*t+71*t*t));
}
static double origin=1000;
static void reference_variant(FCEchoProbe *p, int64_t generation,int gap) {
    int16_t pcm[1440];
    for (int frame=0; frame<72000; frame+=1440) {
        if(gap && frame==1440) continue;
        for(int i=0;i<1440;i++) pcm[i]=(int16_t)lrint(source((frame+i)/24000.0)*32767);
        fc_echo_reference(p,generation,pcm,1440,frame);
    }
}
static void reference(FCEchoProbe *p,int64_t g) { reference_variant(p,g,0); }
static void capture_variant(FCEchoProbe *p, double delay, double gain, double noise,int gap) {
    int16_t pcm[1600];
    unsigned seed=29;
    for(int frame=-8000;frame<64000;frame+=1600) {
        if(gap && frame==1600) continue;
        for(int i=0;i<1600;i++) {
            double t=(frame+i)/16000.0;
            seed=1664525u*seed+1013904223u;
            double n=((seed>>8)/16777216.0-0.5)*noise;
            pcm[i]=(int16_t)lrint((gain*source(t-delay)+n)*32767);
        }
        fc_echo_capture(p,pcm,1600,origin+frame/16000.0);
    }
}
static void capture(FCEchoProbe *p,double delay,double gain,double noise) { capture_variant(p,delay,gain,noise,0); }
static int checks=0;
static void passed(const char *name) { checks++;printf("PASS %s\n",name); }
static FCEchoProbe *prepared(void) {
    FCEchoProbe *p=fc_echo_create();assert(p);fc_echo_begin(p,41,1);reference(p,1);fc_echo_anchor(p,1,origin);return p;
}
static void known_delay(double delay,double gain,double noise) {
    FCEchoProbe *p=prepared();capture(p,delay,gain,noise);fc_echo_end(p,1);
    FCEchoResult r;int count=0;
    while(fc_echo_poll(p,&r)) {
        assert(r.status==FC_ECHO_MEASURED); assert(r.playback_id==41 && r.generation==1);
        if(fabs(r.lag_ms-delay*1000)>=1) fprintf(stderr,"delay mismatch expected %.2f got %.2f window %d correlation %.4f\n",delay*1000,r.lag_ms,r.window_start_ms,r.correlation);
        assert(fabs(r.lag_ms-delay*1000)<1);assert(fabs(r.correlation)>0.9);
        assert(r.correlation*gain>0);assert(fabs(r.projection_gain/gain-1)<0.12);
        assert(r.tested_lags>=1000);count++;
    }
    assert(count==12);fc_echo_destroy(p);
}
int main(void) {
    FCEchoProbe *p=fc_echo_create(); assert(p);
    fc_echo_begin(p,41,1); reference(p,1); fc_echo_anchor(p,1,1000);
    capture(p,0.073,0.2,0.001); fc_echo_end(p,1);
    FCEchoResult r; int measured=0;
    while(fc_echo_poll(p,&r)) if(r.status==FC_ECHO_MEASURED) {
        assert(r.playback_id==41 && r.generation==1);
        assert(fabs(r.lag_ms-73)<1.0); assert(r.correlation>0.95);
        measured++;
    }
    assert(measured>=8);
    fc_echo_destroy(p);
    passed("known delay 73ms with gain and noise");
    p=fc_echo_create();assert(p);fc_echo_begin(p,42,1);reference(p,1);fc_echo_anchor(p,1,1000);
    capture_variant(p,0.073,0.2,0.001,1);fc_echo_end(p,1);
    assert(fc_echo_poll(p,&r)); assert(r.status==FC_ECHO_CAPTURE_GAP);
    fc_echo_destroy(p);passed("capture gap is inconclusive instead of a restricted lag search");
    known_delay(-0.150,0.2,0.001);passed("negative apparent delay");
    known_delay(0,0.2,0.001);passed("zero apparent delay");
    known_delay(0.32125,0.02,0.001);passed("fractional delay and quiet residual");
    known_delay(0.790,-0.2,0.001);passed("long delay with inverted polarity");
    origin=1693000.25;known_delay(0.073,0.2,0.001);origin=1000;passed("large monotonic host times preserve submillisecond lag");
    p=prepared();int frame=-8000,block=0;int16_t split[342];
    while(frame<64000) {
        int n=block%3==2?342:341;if(n>64000-frame)n=64000-frame;
        for(int i=0;i<n;i++) split[i]=(int16_t)lrint(0.2*source((frame+i)/16000.0-0.073)*32767);
        fc_echo_capture(p,split,(size_t)n,999.5+block*1024/48000.0);frame+=n;block++;
    }
    fc_echo_end(p,1);int split_count=0;
    while(fc_echo_poll(p,&r)) { assert(r.status==FC_ECHO_MEASURED);assert(fabs(r.lag_ms-73)<1);split_count++; }
    assert(split_count==12);fc_echo_destroy(p);passed("continuous 341 341 342 chunks and 48k tap timestamps do not create false gaps");
    p=fc_echo_create();assert(p);frame=-8000;block=0;double source_host=999.5;int16_t primed[1728];
    while(frame<64000) {
        if(block==2) {fc_echo_begin(p,41,1);reference(p,1);fc_echo_anchor(p,1,1000);}
        int n=block==0?1728:1600;if(n>64000-frame)n=64000-frame;
        for(int i=0;i<n;i++) primed[i]=(int16_t)lrint(0.2*source((frame+i)/16000.0-0.073)*32767);
        size_t source_frames=block==0?4800:(size_t)n*3;
        fc_echo_capture_source(p,primed,(size_t)n,source_host,source_frames,48000);
        source_host+=source_frames/48000.0;frame+=n;block++;
    }
    fc_echo_end(p,1);int primed_count=0;
    while(fc_echo_poll(p,&r)) {assert(r.status==FC_ECHO_MEASURED && fabs(r.lag_ms-73)<1);primed_count++;}
    FCEchoCaptureStats timing;fc_echo_capture_stats(p,&timing);
    assert(primed_count==12 && timing.source_backwards==0 && fabs(timing.converter_offset_ms-8)<0.001);
    fc_echo_destroy(p);passed("SRC priming before playback does not reverse the source clock");
    p=prepared();frame=-8000;block=0;int16_t drifted[1600];
    while(frame<64000) {
        for(int i=0;i<1600;i++) drifted[i]=(int16_t)lrint(0.2*source((frame+i)/16000.0-0.073)*32767);
        fc_echo_capture_source(p,drifted,1600,999.5+block*0.1001,4800,48000);
        frame+=1600;block++;
    }
    fc_echo_end(p,1);int drift_count=0;
    while(fc_echo_poll(p,&r)) { assert(r.status==FC_ECHO_CLOCK_UNSTABLE);drift_count++; }
    assert(drift_count==12);fc_echo_destroy(p);passed("small per-block host deltas cannot conceal accumulated clock drift");
    p=prepared();frame=-8000;block=0;double buffered_host=999.5;int16_t buffered[1728];
    const int sizes[3]={1360,1728,1712};
    while(frame<64000) {
        int n=sizes[block%3];if(n>64000-frame)n=64000-frame;
        for(int i=0;i<n;i++) buffered[i]=(int16_t)lrint(0.2*source((frame+i)/16000.0-0.073)*32767);
        size_t frames=(frame+n==64000)?(size_t)n*3:4800;
        fc_echo_capture_source(p,buffered,(size_t)n,buffered_host,frames,48000);
        buffered_host+=frames/48000.0;frame+=n;block++;
    }
    fc_echo_end(p,1);int buffered_count=0;
    while(fc_echo_poll(p,&r)) {assert(r.status==FC_ECHO_MEASURED && fabs(r.lag_ms-73)<1);buffered_count++;}
    assert(buffered_count==12);fc_echo_destroy(p);passed("SRC delivery batching is not source clock drift");
    p=fc_echo_create();assert(p);frame=-16000;block=0;buffered_host=999;
    while(frame<64000) {
        if(block==8) {fc_echo_begin(p,41,1);reference(p,1);fc_echo_anchor(p,1,1000);}
        int n=sizes[block%3];if(n>64000-frame)n=64000-frame;
        for(int i=0;i<n;i++) buffered[i]=(int16_t)lrint(0.2*source((frame+i)/16000.0-0.073)*32767);
        size_t frames=(frame+n==64000)?(size_t)n*3:4800;
        if(block!=1) fc_echo_capture_source(p,buffered,(size_t)n,buffered_host,frames,48000);
        buffered_host+=frames/48000.0;frame+=n;block++;
    }
    fc_echo_end(p,1);int lost_count=0;
    while(fc_echo_poll(p,&r)) {assert(r.status==FC_ECHO_CAPTURE_GAP);lost_count++;}
    assert(lost_count==12);fc_echo_destroy(p);passed("missing SRC delivery before playback cannot leave a silently shifted sample clock");

    p=prepared();capture(p,0,0,0.02);fc_echo_end(p,1);
    while(fc_echo_poll(p,&r)) { assert(r.status==FC_ECHO_MEASURED); assert(fabs(r.correlation)<0.30); }
    fc_echo_destroy(p);passed("unrelated noise has low correlation");
    p=prepared();capture(p,0,0,0);fc_echo_end(p,1);
    while(fc_echo_poll(p,&r)) assert(r.status==FC_ECHO_LOW_CAPTURE);
    fc_echo_destroy(p);passed("silent capture is inconclusive");

    p=fc_echo_create();assert(p);fc_echo_begin(p,41,1);
    int16_t zero[1440]={0};for(int frame=0;frame<72000;frame+=1440) fc_echo_reference(p,1,zero,1440,frame);
    fc_echo_anchor(p,1,1000);capture(p,0.073,0.2,0.001);fc_echo_end(p,1);
    while(fc_echo_poll(p,&r)) assert(r.status==FC_ECHO_LOW_REFERENCE);
    fc_echo_destroy(p);passed("silent reference is inconclusive");

    p=prepared();fc_echo_anchor(p,1,1000.04);capture(p,0.073,0.2,0.001);fc_echo_end(p,1);
    while(fc_echo_poll(p,&r)) { assert(r.status==FC_ECHO_CLOCK_UNSTABLE); assert(r.anchor_spread_ms>39); }
    fc_echo_destroy(p);passed("reference clock jump is inconclusive");

    p=fc_echo_create();assert(p);fc_echo_begin(p,41,1);reference(p,1);fc_echo_end(p,1);fc_echo_close(p);
    while(fc_echo_poll(p,&r)) assert(r.status==FC_ECHO_CLOCK_MISSING);
    fc_echo_destroy(p);passed("missing render clock is inconclusive");

    p=fc_echo_create();assert(p);fc_echo_begin(p,41,1);reference(p,1);capture(p,0.073,0.2,0.001);fc_echo_end(p,1);
    assert(!fc_echo_poll(p,&r));fc_echo_anchor(p,1,1000);
    assert(fc_echo_poll(p,&r) && r.status==FC_ECHO_MEASURED);fc_echo_destroy(p);passed("late immutable clock snapshot can follow playback end");

    p=fc_echo_create();assert(p);fc_echo_begin(p,41,1);reference_variant(p,1,1);fc_echo_anchor(p,1,1000);capture(p,0.073,0.2,0.001);fc_echo_end(p,1);
    assert(fc_echo_poll(p,&r) && r.status==FC_ECHO_REFERENCE_GAP);fc_echo_destroy(p);passed("reference underrun is not filled with invented audio");

    p=prepared();int16_t dc[1600];for(int i=0;i<1600;i++) dc[i]=1000;
    for(int frame=-8000;frame<64000;frame+=1600) fc_echo_capture(p,dc,1600,1000+frame/16000.0);
    fc_echo_end(p,1);while(fc_echo_poll(p,&r)) assert(r.status==FC_ECHO_LOW_CAPTURE);
    fc_echo_destroy(p);passed("DC input does not masquerade as correlated speech");

    p=prepared();fc_echo_end(p,1);assert(!fc_echo_poll(p,&r));fc_echo_close(p);
    while(fc_echo_poll(p,&r)) assert(r.status==FC_ECHO_INCOMPLETE);
    fc_echo_destroy(p);passed("stop before tail capture marks incomplete");

    p=prepared();fc_echo_end(p,1);fc_echo_begin(p,42,3);
    int16_t inverted[1440];for(int f=0;f<72000;f+=1440) {
        for(int i=0;i<1440;i++) inverted[i]=(int16_t)lrint(-source((f+i)/24000.0)*32767);
        fc_echo_reference(p,3,inverted,1440,f);
    }
    fc_echo_anchor(p,3,1000.1);
    capture(p,0.073,0.2,0.001);fc_echo_end(p,3);int old=0,fresh=0;
    while(fc_echo_poll(p,&r)) {
        assert(r.status==FC_ECHO_MEASURED);
        if(r.generation==1) {assert(r.playback_id==41 && r.correlation>0.95 && fabs(r.lag_ms-73)<1);old++;}
        else {assert(r.generation==3 && r.playback_id==42 && r.correlation < -0.95 && fabs(r.lag_ms+27)<1);fresh++;}
    }
    assert(old==12 && fresh==12);fc_echo_destroy(p);passed("playback identities remain separate");

    p=fc_echo_create();assert(p);fc_echo_begin(p,41,1);int f=0,k=0;int16_t fragmented[1441];
    while(f<72000) {
        int n=k%2?1441:1439;if(n>72000-f)n=72000-f;
        for(int i=0;i<n;i++) fragmented[i]=(int16_t)lrint(source((f+i)/24000.0)*32767);
        fc_echo_reference(p,1,fragmented,(size_t)n,f);f+=n;k++;
    }
    fc_echo_anchor(p,1,1000);capture(p,0.073,0.2,0.001);fc_echo_end(p,1);int fragmented_count=0;
    while(fc_echo_poll(p,&r)) {assert(r.status==FC_ECHO_MEASURED && fabs(r.lag_ms-73)<1);fragmented_count++;}
    assert(fragmented_count==12);fc_echo_destroy(p);passed("reference sample phase survives nonmultiple chunk sizes");

    p=fc_echo_create();assert(p);fc_echo_begin(p,41,1);fc_echo_begin(p,42,3);fc_echo_begin(p,43,5);
    assert(fc_echo_dropped_playbacks(p)==1);reference(p,1);reference(p,3);reference(p,5);
    fc_echo_anchor(p,3,1000);fc_echo_anchor(p,5,1000);capture(p,0.073,0.2,0.001);fc_echo_end(p,3);fc_echo_end(p,5);
    while(fc_echo_poll(p,&r)) assert(r.playback_id!=41);
    fc_echo_destroy(p);passed("bounded playback capacity evicts explicitly");

    p=prepared();capture(p,0.073,0.2,0.001);fc_echo_end(p,1);
    for(int i=0;i<90;i++) fc_echo_capture(p,zero,1440,1004+i*0.09);
    assert(fc_echo_poll(p,&r) && r.status==FC_ECHO_CAPTURE_GAP);
    fc_echo_destroy(p);passed("overwritten input is a gap, not stale audio");

    p=fc_echo_create();assert(p);fc_echo_begin(p,41,1);fc_echo_reference(p,1,zero,96,0);fc_echo_anchor(p,1,1000);fc_echo_end(p,1);fc_echo_close(p);
    assert(fc_echo_poll(p,&r) && r.status==FC_ECHO_SHORT_WINDOW);fc_echo_destroy(p);passed("very short reference is inconclusive");

    p=prepared();capture(p,0.073,0.2,0.001);fc_echo_capture(p,zero,1440,1000);fc_echo_end(p,1);
    assert(fc_echo_poll(p,&r) && r.status==FC_ECHO_CLOCK_UNSTABLE);fc_echo_destroy(p);passed("capture clock reversal is inconclusive");

    p=fc_echo_create();assert(p);fc_echo_begin(p,41,1);reference(p,99);
    fc_echo_reference(p,1,zero,(size_t)-1,0);fc_echo_reference(p,1,zero,1440,INT64_MAX);
    fc_echo_capture(p,zero,1440,NAN);fc_echo_capture(p,zero,(size_t)-1,1000);fc_echo_anchor(p,1,INFINITY);
    fc_echo_close(p);assert(!fc_echo_poll(p,&r));fc_echo_destroy(p);fc_echo_destroy(NULL);passed("invalid input lengths clocks and generations are ignored");
    printf("PASS %d cases\n",checks);
    return 0;
}
