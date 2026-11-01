# Included after project(): IDF has finished creating every component target.
function(family_c3_pcm_build)
    if(NOT CONFIG_XMINI_C3_PCM OR CMAKE_BUILD_EARLY_EXPANSION)
        return()
    endif()
    if(NOT IDF_TARGET STREQUAL "esp32c3" OR NOT CONFIG_BOARD_TYPE_XMINI_C3
       OR NOT CONFIG_XMINI_C3_BUTTON_ENTRY OR NOT CONFIG_XMINI_C3_VOICE_BARGE
       OR NOT BOARD_NAME STREQUAL "xmini-c3-family"
       OR NOT CONFIG_LANGUAGE_ZH_CN OR NOT CONFIG_FLASH_DEFAULT_ASSETS)
        message(FATAL_ERROR "C3 PCM requires xmini-c3-family/esp32c3, zh-CN and default assets")
    endif()
    idf_build_get_property(family_root PROJECT_DIR)
    idf_build_get_property(family_python PYTHON)
    if(NOT family_root OR NOT family_python)
        message(FATAL_ERROR "IDF project directory/Python build property missing")
    endif()
    set(recipe "${family_root}/scripts/family_c3_build.py")
    set(cues "${CMAKE_BINARY_DIR}/family_c3_pcm")
    set(network "${CMAKE_BINARY_DIR}/family_c3_network")
    execute_process(COMMAND "${family_python}" "${recipe}" --source-root "${family_root}" --check-sources
        RESULT_VARIABLE checked)
    if(NOT checked EQUAL 0)
        message(FATAL_ERROR "C3 PCM approved cue inventory validation failed")
    endif()
    idf_component_get_property(main_lib main COMPONENT_LIB)
    idf_component_get_property(network_lib 78__esp-ml307 COMPONENT_LIB)
    idf_component_get_property(component 78__esp-ml307 COMPONENT_DIR)
    idf_build_get_property(family_idf IDF_PATH)
    # Native offsetof evidence needs the HAL headers hidden by esp_timer's private dependency.
    target_include_directories("${main_lib}" PRIVATE
        "${family_idf}/components/esp_driver_i2s" "${family_idf}/components/esp_driver_dma/src"
        "${family_idf}/components/esp_hal_systimer/include"
        "${family_idf}/components/esp_hal_systimer/esp32c3/include")
    target_link_options("${main_lib}" INTERFACE
        "-Wl,--emit-relocs" "-Wl,--undefined=family_c3_sdk_layout"
        "-Wl,--wrap=spi_flash_disable_interrupts_caches_and_other_cpu"
        "-Wl,--wrap=spi_flash_enable_interrupts_caches_and_other_cpu")
    if(NOT TARGET "${main_lib}" OR NOT TARGET "${network_lib}" OR NOT TARGET flash)
        message(FATAL_ERROR "C3 PCM hook must run after IDF component and flash target creation")
    endif()
    # Fail at configure time on dependency drift, before modifying target inputs.
    execute_process(COMMAND "${family_python}" "${family_root}/scripts/family_c3_network.py"
        --component-root "${component}" --output-dir "${network}" RESULT_VARIABLE patched)
    if(NOT patched EQUAL 0)
        message(FATAL_ERROR "C3 PCM managed network input validation failed")
    endif()
    get_target_property(native_sources "${network_lib}" SOURCES)
    get_target_property(native_dir "${network_lib}" SOURCE_DIR)
    set(selected "${native_sources}")
    foreach(pair IN ITEMS "src/web_socket.cc|web_socket.cc" "src/esp/esp_tcp.cc|esp_tcp.cc")
        string(REPLACE "|" ";" pair "${pair}")
        list(GET pair 0 original)
        list(GET pair 1 generated)
        set(matches 0)
        set(next_sources "")
        foreach(source IN LISTS selected)
            get_filename_component(absolute "${source}" ABSOLUTE BASE_DIR "${native_dir}")
            if(absolute STREQUAL "${component}/${original}")
                math(EXPR matches "${matches}+1")
                list(APPEND next_sources "${network}/${generated}")
            else()
                list(APPEND next_sources "${source}")
            endif()
        endforeach()
        if(NOT matches EQUAL 1)
            message(FATAL_ERROR "C3 PCM source replacement requires exactly one ${original}; found ${matches}")
        endif()
        set(selected "${next_sources}")
    endforeach()
    set_property(TARGET "${network_lib}" PROPERTY SOURCES "${selected}")
    set_source_files_properties("${network}/web_socket.cc" "${network}/esp_tcp.cc" PROPERTIES GENERATED TRUE)
    target_include_directories("${network_lib}" PRIVATE "${family_root}/main/audio")
    # Existing component '.' private include remains: generated TCP uses src/esp/esp_tcp.h.
    add_custom_target(family_c3_network_generated
        COMMAND "${family_python}" "${family_root}/scripts/family_c3_network.py"
            --component-root "${component}" --output-dir "${network}"
        BYPRODUCTS "${network}/web_socket.cc" "${network}/esp_tcp.cc"
            "${network}/family_c3_network.manifest.json" "${network}/LICENSE.esp-ml307"
        VERBATIM)
    add_dependencies("${network_lib}" family_c3_network_generated)
    add_custom_target(family_c3_pcm_generated ALL
        COMMAND "${family_python}" "${recipe}" --source-root "${family_root}" --output-dir "${cues}"
        BYPRODUCTS "${cues}/family_c3_pcm_cues.pack" "${cues}/family_c3_pcm_cues.json"
            "${cues}/family_c3_pcm_cues_generated.h"
        VERBATIM)
    target_include_directories("${main_lib}" PRIVATE "${cues}")
    add_dependencies("${main_lib}" family_c3_pcm_generated)
    # IDF uses generator expressions over the live flash target properties.
    # Four-argument API verified against the pinned IDF project_include.cmake.
    esptool_py_flash_target_image(flash family_c3_pcm 0x650000 "${cues}/family_c3_pcm_cues.pack")
    add_dependencies(flash family_c3_pcm_generated)
    if(TARGET merge-bin)
        add_dependencies(merge-bin family_c3_pcm_generated)
    endif()
    set_property(TARGET "${main_lib}" "${network_lib}" PROPERTY EXPORT_COMPILE_COMMANDS ON)
    set(CMAKE_EXPORT_COMPILE_COMMANDS ON CACHE BOOL "Retain actual C3 compiled inputs" FORCE)
    file(GENERATE OUTPUT "${network}/compiled-sources.txt"
         CONTENT "$<JOIN:$<TARGET_PROPERTY:${network_lib},SOURCES>,\n>\n")
endfunction()

# Keep original DATA_FILE and SDK emitter semantics; only the numeric ASM output name changes.
function(family_c3_pcm_embed_files target)
    if(NOT CONFIG_XMINI_C3_PCM OR CMAKE_BUILD_EARLY_EXPANSION)
        return()
    endif()
    idf_build_get_property(build_dir BUILD_DIR)
    idf_build_get_property(idf_path IDF_PATH)
    foreach(input IN LISTS ARGN)
        get_filename_component(input "${input}" ABSOLUTE)
        get_filename_component(name "${input}" NAME)
        if(name MATCHES "^[0-9]+[.]ogg$")
            set(assembly "${build_dir}/family_c3_embedded_${name}.S")
            set(emitter "${idf_path}/tools/cmake/scripts/data_file_embed_asm.cmake")
            add_custom_command(OUTPUT "${assembly}"
                COMMAND "${CMAKE_COMMAND}" -D "DATA_FILE=${input}" -D "SOURCE_FILE=${assembly}"
                    -D "FILE_TYPE=BINARY" -P "${emitter}"
                MAIN_DEPENDENCY "${input}" DEPENDS "${emitter}"
                WORKING_DIRECTORY "${build_dir}" VERBATIM)
            set_property(DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}" APPEND PROPERTY ADDITIONAL_CLEAN_FILES "${assembly}")
            target_sources("${target}" PRIVATE "${assembly}")
        else()
            target_add_binary_data("${target}" "${input}" BINARY)
        endif()
    endforeach()
endfunction()
