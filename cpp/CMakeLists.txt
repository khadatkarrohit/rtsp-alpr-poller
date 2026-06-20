cmake_minimum_required(VERSION 3.16)
project(rtsp_alpr_poller)

set(CMAKE_CXX_STANDARD 17)

find_package(OpenCV REQUIRED)
find_package(CURL REQUIRED)

add_executable(poller poller.cpp)
target_link_libraries(poller PRIVATE ${OpenCV_LIBS} CURL::libcurl)
target_include_directories(poller PRIVATE ${OpenCV_INCLUDE_DIRS})

# Pi optimisation: enable NEON on ARM
if(CMAKE_SYSTEM_PROCESSOR MATCHES "aarch64|armv7")
  target_compile_options(poller PRIVATE -O3 -march=native)
endif()
