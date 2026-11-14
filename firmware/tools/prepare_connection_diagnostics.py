"""Observe common factory-S3 voice connections without changing their policy."""


def replace(path, old, new):
    text = path.read_text()
    if text.count(old) != 1:
        raise ValueError('connection diagnostic source anchor mismatch: ' + path.name)
    path.write_text(text.replace(old, new))


def prepare_connection_diagnostics(tree):
    application = tree / 'main/application.cc'
    replace(application, '#include "application.h"',
            '#include "application.h"\n#include "connection_trace.h"')
    for method, stage in [('HandleToggleChatEvent', 'Button'), ('HandleStartListeningEvent', 'StartButton')]:
        anchor = f'void Application::{method}() {{\n    auto state = GetDeviceState();'
        replace(application, anchor, anchor +
                f'\n    ConnectionDiagnostics::Observe(ConnectionDiagnostics::Stage::{stage}, static_cast<int>(state));')
        # Keep the entry visible even when Connecting ignores it, but allocate
        # only in the existing branch that schedules a physical channel open.
        text = application.read_text()
        start = text.index(f'void Application::{method}() {{')
        end = text.index('\nvoid Application::', start + 1)
        original = text[start:end]
        anchor = '        if (!protocol_->IsAudioChannelOpened()) {'
        if original.count(anchor) != 1:
            raise ValueError('connection diagnostic closed-channel anchor mismatch: ' + method)
        updated = original.replace(anchor, anchor +
            f'\n            ConnectionDiagnostics::Begin(ConnectionDiagnostics::Stage::{stage}, static_cast<int>(state));')
        replace(application, original, updated)
    anchor = 'void Application::BeginWakeWordInvoke(const std::string& wake_word) {'
    replace(application, anchor, anchor +
            '\n    ConnectionDiagnostics::Observe(ConnectionDiagnostics::Stage::Wake, static_cast<int>(GetDeviceState()));')
    anchor = '    if (!protocol_->IsAudioChannelOpened()) {\n        // Schedule to let the state change be processed first (UI update),'
    replace(application, anchor,
            '    if (!protocol_->IsAudioChannelOpened()) {\n'
            '        ConnectionDiagnostics::Begin(ConnectionDiagnostics::Stage::Wake, static_cast<int>(GetDeviceState()));\n'
            '        // Schedule to let the state change be processed first (UI update),')
    for signature in ['void Application::ContinueOpenAudioChannel(ListeningMode mode) {',
                      'void Application::ContinueWakeWordInvoke(const std::string& wake_word) {']:
        replace(application, signature, signature +
                '\n    ConnectionDiagnostics::Observe(ConnectionDiagnostics::Stage::Continue, static_cast<int>(GetDeviceState()));')
    anchor = '    if (!state_machine_.TransitionTo(state))\n        return false;'
    replace(application, anchor, anchor +
            '\n    ConnectionDiagnostics::Observe(ConnectionDiagnostics::Stage::State, static_cast<int>(state));')
    anchor = 'void Application::RecoverListeningFailure() {'
    replace(application, anchor, anchor +
            '\n    ConnectionDiagnostics::Observe(ConnectionDiagnostics::Stage::Recovery, static_cast<int>(GetDeviceState()));')
    anchor = 'void Application::SetListeningMode(ListeningMode mode) {\n    listening_mode_ = mode;\n    SetDeviceState(kDeviceStateListening);'
    replace(application, anchor, anchor +
            '\n    if (GetDeviceState() == kDeviceStateListening)\n'
            '        ConnectionDiagnostics::Observe(ConnectionDiagnostics::Stage::Listening, static_cast<int>(GetDeviceState()));')

    header = tree / 'main/protocols/websocket_protocol.h'
    replace(header, '#include "protocol.h"', '#include "protocol.h"\n#include "connection_trace_network.h"')
    replace(header, '    std::unique_ptr<WebSocket> websocket_;',
            '    // The socket must be destroyed before its forwarding network adapter.\n'
            '    std::unique_ptr<ConnectionDiagnostics::Network> connection_trace_network_;\n'
            '    std::unique_ptr<WebSocket> websocket_;')
    protocol = tree / 'main/protocols/websocket_protocol.cc'
    replace(protocol, 'bool WebsocketProtocol::OpenAudioChannel() {\n    if (!ResetPlaybackConnection()) return false;',
            'bool WebsocketProtocol::OpenAudioChannel() {\n'
            '    const auto trace_attempt = ConnectionDiagnostics::ForSocket();\n'
            '    ConnectionDiagnostics::Mark(trace_attempt, ConnectionDiagnostics::Stage::Open);\n'
            '    if (!ResetPlaybackConnection()) {\n'
            '        ConnectionDiagnostics::Mark(trace_attempt, ConnectionDiagnostics::Stage::ResetFailed);\n'
            '        return false;\n'
            '    }\n'
            '    ConnectionDiagnostics::Mark(trace_attempt, ConnectionDiagnostics::Stage::ResetReady);')
    replace(protocol, '    websocket_ = network->CreateWebSocket(1);',
            '    if (!connection_trace_network_) connection_trace_network_ = std::make_unique<ConnectionDiagnostics::Network>(network);\n'
            '    connection_trace_network_->SetAttempt(trace_attempt);\n'
            '    ConnectionDiagnostics::Mark(trace_attempt, ConnectionDiagnostics::Stage::SocketCreate);\n'
            '    websocket_ = connection_trace_network_->CreateWebSocket(1);')
    replace(protocol, '    if (websocket_ == nullptr) {',
            '    if (websocket_ == nullptr) {\n'
            '        ConnectionDiagnostics::Mark(trace_attempt, ConnectionDiagnostics::Stage::SocketFailed);')
    anchor = '    websocket_->SetHeader("Protocol-Version", std::to_string(version_).c_str());'
    replace(protocol, anchor, anchor +
            '\n    if (trace_attempt) websocket_->SetHeader("X-Robot-Connection-Id", trace_attempt.id);')
    replace(protocol, 'websocket_->OnData([this, connection]', 'websocket_->OnData([this, connection, trace_attempt]')
    replace(protocol, '                    ParseServerHello(root, connection);',
            '                    ConnectionDiagnostics::Mark(trace_attempt, ConnectionDiagnostics::Stage::HelloReceived);\n'
            '                    ParseServerHello(root, connection);')
    replace(protocol, 'websocket_->OnDisconnected([this, connection]() {',
            'websocket_->OnDisconnected([this, connection, trace_attempt]() {\n'
            '        ConnectionDiagnostics::Mark(trace_attempt, ConnectionDiagnostics::Stage::SocketClosed);')
    anchor = '    if (auto connected = websocket_->Connect(url.c_str()); !connected) {'
    replace(protocol, anchor, anchor +
            '\n        ConnectionDiagnostics::Mark(trace_attempt, ConnectionDiagnostics::Stage::ConnectFailed,\n'
            '            static_cast<int>(connected.error().code), connected.error().native);')
    anchor = '    // Send hello message to describe the client'
    replace(protocol, anchor, '    ConnectionDiagnostics::Mark(trace_attempt, ConnectionDiagnostics::Stage::Connected);\n\n' + anchor)
    replace(protocol, '    if (!SendText(message)) {\n        return false;\n    }',
            '    if (!SendText(message)) {\n'
            '        ConnectionDiagnostics::Mark(trace_attempt, ConnectionDiagnostics::Stage::HelloFailed);\n'
            '        return false;\n    }\n'
            '    ConnectionDiagnostics::Mark(trace_attempt, ConnectionDiagnostics::Stage::HelloSent);')
    anchor = '            ESP_LOGE(TAG, "Failed to receive server hello");'
    replace(protocol, anchor,
            '            ConnectionDiagnostics::Mark(trace_attempt, ConnectionDiagnostics::Stage::HelloTimeout, 10000);\n' + anchor)
    anchor = '        // Server hello accepted while holding the current connection lock.'
    replace(protocol, anchor,
            '        ConnectionDiagnostics::Mark(trace_attempt, ConnectionDiagnostics::Stage::HelloReady);\n' + anchor)
    anchor = ('            !websocket_->IsConnected()) {\n'
              '            SetError(Lang::Strings::SERVER_ERROR);')
    replace(protocol, anchor,
            '            !websocket_->IsConnected()) {\n'
            '            ConnectionDiagnostics::Mark(trace_attempt, ConnectionDiagnostics::Stage::HelloFailed);\n'
            '            SetError(Lang::Strings::SERVER_ERROR);')
