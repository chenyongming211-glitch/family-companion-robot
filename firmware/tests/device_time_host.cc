// Only settimeofday is intercepted; JSON parsing, startup policy and formatting are real.
#include "cJSON.h"
#include "desktop_clock.h"
// PRODUCTION_TIME_HEADER
#include <cassert>
#include <cmath>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <limits>
#include <sys/time.h>

static timeval system_time{};
static unsigned time_writes=0;
static int HostSetTime(const timeval* value,void*) {system_time=*value;++time_writes;return 0;}
#define settimeofday HostSetTime
#define ESP_LOGW(...) ((void)0)
static bool ApplyServerTime(cJSON* root) {
    bool has_server_time_=false;
// PRODUCTION_TIME_BLOCK
    return has_server_time_;
}
static void StartupBeforeBoard() {
// PRODUCTION_STARTUP_PRELUDE
}
static void Apply(const char* offset) {
    char payload[192];
    std::snprintf(payload,sizeof(payload),"{\"server_time\":{\"timestamp\":1794488400123%s%s}}",
        offset?",\"timezone_offset\":" : "",offset?offset:"");
    auto* root=cJSON_Parse(payload);assert(root);assert(ApplyServerTime(root));cJSON_Delete(root);
}
static void Expect(time_t utc,const char* time,const char* date) {
    const auto shown=family_robot::FormatDesktopClock(utc);
    if(!shown.valid || std::strcmp(shown.time,time) || std::strcmp(shown.date,date)) {
        std::fprintf(stderr,"UTC %lld expected %s / %s, got %s / %s\n",
            static_cast<long long>(utc),time,date,shown.time,shown.date);std::abort();
    }
}
int main(int argc,char** argv) {
    assert(argc==2);tzset();StartupBeforeBoard();
    if(!std::strcmp(argv[1],"utc")) {
        for(int i=0;i<4;++i) {
            Apply("480");
            if(system_time.tv_sec!=1794488400 || system_time.tv_usec!=123000) {
                std::fprintf(stderr,"OTA must write UTC 1794488400.123000; got %lld.%06ld\n",
                    static_cast<long long>(system_time.tv_sec),static_cast<long>(system_time.tv_usec));return 1;
            }
        }
        assert(time_writes==4);
    } else if(!std::strcmp(argv[1],"sntp")) {
        Apply("480");Expect(system_time.tv_sec,"21:00","11月12日 星期四");
        // SNTP writes the standard Unix epoch after OTA. It must not undo Beijing display.
        timeval sntp{1794488400,123000};HostSetTime(&sntp,nullptr);
        Expect(system_time.tv_sec,"21:00","11月12日 星期四");
        Expect(1794499199,"23:59","11月12日 星期四");
        Expect(1794499200,"00:00","11月13日 星期五");
        Expect(1794672000,"00:00","11月15日 星期日");
    } else if(!std::strcmp(argv[1],"startup")) {
        // No OTA response: an early SNTP result already displays Beijing.
        assert(time_writes==0);Expect(1794488400,"21:00","11月12日 星期四");
        const auto unset=family_robot::FormatDesktopClock(0);assert(!unset.valid && !std::strcmp(unset.time,"--:--"));
    } else if(!std::strcmp(argv[1],"offsets")) {
        for(const char* offset:{"345","-210","840","-840","0"}) {
            Apply(offset);assert(system_time.tv_sec==1794488400);
            const char* times[]={"18:45","09:30","03:00","23:00","13:00"};
            const char* dates[]={"11月12日 星期四","11月12日 星期四","11月13日 星期五","11月11日 星期三","11月12日 星期四"};
            const int index=!std::strcmp(offset,"345")?0:!std::strcmp(offset,"-210")?1:
                !std::strcmp(offset,"840")?2:!std::strcmp(offset,"-840")?3:4;
            Expect(system_time.tv_sec,times[index],dates[index]);
        }
        Apply("480");
        for(const char* invalid:{static_cast<const char*>(nullptr),"null","\"480\"","true","480.5","841","-841","1e300","{}","[]"}) {
            Apply(invalid);assert(system_time.tv_sec==1794488400);
            Expect(system_time.tv_sec,"21:00","11月12日 星期四");
        }
        for(double invalid:{std::numeric_limits<double>::infinity(),-std::numeric_limits<double>::infinity(),
                            std::numeric_limits<double>::quiet_NaN()}) {
            auto* root=cJSON_Parse("{\"server_time\":{\"timestamp\":1794488400123,\"timezone_offset\":0}}");
            assert(root);cJSON_GetObjectItem(cJSON_GetObjectItem(root,"server_time"),"timezone_offset")->valuedouble=invalid;
            assert(ApplyServerTime(root));cJSON_Delete(root);
            assert(system_time.tv_sec==1794488400);Expect(system_time.tv_sec,"21:00","11月12日 星期四");
        }
        for(const char* missing:{"{}","{\"server_time\":null}","{\"server_time\":{\"timezone_offset\":0}}"}) {
            const auto before=time_writes;auto* root=cJSON_Parse(missing);assert(root);
            assert(!ApplyServerTime(root));cJSON_Delete(root);assert(time_writes==before);
            Expect(system_time.tv_sec,"21:00","11月12日 星期四");
        }
    } else return 2;
    std::puts("UTC system clock and configured local display passed");
}
