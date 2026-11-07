#include "probe.h"
#include <cassert>
#include <cmath>

int main() {
    using namespace wave_wake_lab;
    assert(threshold_percent.load()==20);
    assert(Parse("wwd status").kind==Kind::Status);
    assert(Parse("wwd start").kind==Kind::Start);
    assert(Parse("wwd stop").kind==Kind::Stop);
    assert(Parse("wwd probe 1").seconds==1);
    assert(Parse("wwd probe 10").seconds==10);
    assert(Parse("wwd threshold 15").seconds==15);
    assert(Parse("wwd threshold 20").seconds==20);
    assert(Parse("wwd threshold 15").kind==Kind::Threshold);
    for(auto s:{"wwd probe 0","wwd probe 11","wwd probe -1","wwd probe 3 extra","wwd start; stop","reboot",""})
        assert(Parse(s).kind==Kind::Invalid);
    for(auto s:{"wwd threshold 0","wwd threshold 14","wwd threshold 16","wwd threshold 21","wwd threshold 15 extra"})
        assert(Parse(s).kind==Kind::Invalid);
    Stats a;
    const int16_t dc[]{100,100,100};
    a.Add(dc,3,1);assert(a.samples==3&&a.peak==100&&a.energy==30000&&a.sum==300);
    assert(a.AcRms()==0&&a.Rms()==100);
    const int16_t extrema[]{-32768,32767};
    Stats b;b.Add(extrema,2,1);assert(b.peak==32768&&b.clipped==2&&b.sum==-1);
    const int16_t stereo[]{200,9,-200,9};
    Stats c;c.Add(stereo,2,2);assert(c.samples==2&&c.AcRms()==200&&c.peak==200);
    Window w;assert(w.Token(100)==0);assert(!w.Begin(100,0)&&!w.Begin(100,11));
    assert(w.Begin(100,1));auto token=w.Token(100);assert(token!=0);
    assert(!w.Begin(200,1));w.Merge(token,0,a);w.Merge(token,2,b);
    w.Count(token,Step::WakeEnabled);w.Count(token,Step::Infer);
    w.Count(token,Step::Infer);w.Count(token,Step::Timeout);
    auto first=w.Take(100000);assert(first.id==token&&!first.done&&first.channels[0].samples==3&&first.channels[2].clipped==2);
    assert(first.steps[size_t(Step::WakeEnabled)]==1);
    assert(first.steps[size_t(Step::Infer)]==2&&first.steps[size_t(Step::Timeout)]==1);
    auto empty=w.Take(200000);assert(empty.channels[0].samples==0);
    for(auto count:empty.steps)assert(count==0);
    assert(w.Token(1000100)==0);auto last=w.Take(1000100);assert(last.done&&last.id==token);
    assert(w.Take(1000101).id==0);assert(w.Begin(2000000,2));
    w.Merge(token,0,a);assert(w.Take(2000001).channels[0].samples==0); // retired write cannot contaminate a new window
    w.Count(token,Step::Infer);w.Count(w.Token(2000001),Step::Stopped);
    w.Count(w.Token(2000001),static_cast<Step>(99));
    auto current=w.Take(2000002);
    assert(current.steps[size_t(Step::Infer)]==0&&current.steps[size_t(Step::Stopped)]==1);
}
