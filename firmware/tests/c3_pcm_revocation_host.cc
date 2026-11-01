#include "button_uplink.h"
#include <cassert>
#include <chrono>
#include <condition_variable>
#include <future>
#include <mutex>
#include <type_traits>
#include <utility>
template<class T,class=void> struct RevokeApi { static bool Run(T&,uint32_t){return false;} };
template<class T> struct RevokeApi<T,std::void_t<decltype(std::declval<T&>().Revoke(uint32_t{}))>> {
 static bool Run(T& gate,uint32_t token) { gate.Revoke(token);return true; }
};
int main() {
 family_robot::ButtonUplink gate;assert(gate.Open(0,[]{}));auto token=gate.Capture();
 std::mutex mutex;std::condition_variable cv;bool entered=false,release=false;
 auto send=std::async(std::launch::async,[&]{return gate.Send(token,[&]{
  std::unique_lock<std::mutex> lock(mutex);entered=true;cv.notify_all();cv.wait(lock,[&]{return release;});return true;});});
 {std::unique_lock<std::mutex> lock(mutex);cv.wait(lock,[&]{return entered;});}
 auto revoked=std::async(std::launch::async,[&]{assert(RevokeApi<family_robot::ButtonUplink>::Run(gate,token));});
 assert(revoked.wait_for(std::chrono::milliseconds(100))==std::future_status::ready);revoked.get();
 assert(!gate.Allows(token)&&gate.Capture()==0);
 {std::lock_guard<std::mutex> lock(mutex);release=true;}cv.notify_all();assert(send.get());
 gate.Close([]{});assert(gate.Open(gate.Revision(),[]{}));const auto newer=gate.Capture();
 assert(newer>token&&gate.Allows(newer));RevokeApi<family_robot::ButtonUplink>::Run(gate,token);assert(gate.Allows(newer));
 RevokeApi<family_robot::ButtonUplink>::Run(gate,newer);RevokeApi<family_robot::ButtonUplink>::Run(gate,token);assert(!gate.Allows(newer));
}
