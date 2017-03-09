#
#  Copyright 2015-2016 Bleemeo
#
#  bleemeo.com an infrastructure monitoring solution in the Cloud
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.
#

import threading

import flask
import jinja2.filters

import bleemeo_agent.checker


app = flask.Flask(__name__)
app_thread = None


@app.route('/')
def home():
    loads = bleemeo_agent.util.get_loadavg(app.core)
    check_info = _gather_checks_info()
    top_output = bleemeo_agent.util.get_top_output(app.core.top_info)
    disks_used_perc = [
        metric
        for metric in app.core.last_metrics.values()
        if metric['measurement'] == 'disk_used_perc'
    ]
    nets_bits_recv = [
        metric
        for metric in app.core.last_metrics.values()
        if metric['measurement'] == 'net_bits_recv'
    ]

    uptime_seconds = bleemeo_agent.util.get_uptime()
    uptime_string = bleemeo_agent.util.format_uptime(uptime_seconds)

    return flask.render_template(
        'index.html',
        core=app.core,
        loads=' '.join('%.2f' % x for x in loads),
        uptime=uptime_string,
        check_info=check_info,
        top_output=top_output,
        disks_used_perc=disks_used_perc,
        nets_bits_recv=nets_bits_recv,
    )


def _gather_checks_info():
    check_count_ok = 0
    check_count_warning = 0
    check_count_critical = 0
    checks = []
    for metric in app.core.last_metrics.values():
        if (metric.get('status') is not None
                and metric.get('status_of') is None):
            if metric['status'] == 'ok':
                check_count_ok += 1
            elif metric['status'] == 'warning':
                check_count_warning += 1
            else:
                check_count_critical += 1
            threshold = app.core.get_threshold(
                metric['measurement'], metric.get('item'),
            )

            pretty_name = metric['measurement']
            if metric.get('item') is not None:
                pretty_name = '%s for %s' % (pretty_name, metric['item'])
            checks.append({
                'name': metric['measurement'],
                'pretty_name': pretty_name,
                'item': metric.get('item'),
                'status': metric.get('status'),
                'value': metric['value'],
                'threshold': threshold,
            })

    return {
        'checks': checks,
        'count_ok': check_count_ok,
        'count_warning': check_count_warning,
        'count_critical': check_count_critical,
        'count_total': len(checks),
    }


@app.route('/check')
def check():
    check_info = _gather_checks_info()

    return flask.render_template(
        'check.html',
        core=app.core,
        check_info=check_info,
    )


@app.template_filter('netsizeformat')
def filter_netsizeformat(value):
    """ Same as standard filesizeformat but for network.

        Convert to human readable network bandwidth (e.g 13 kbps, 4.1 Mbps...)
    """
    return (jinja2.filters.do_filesizeformat(value * 8, False)
            .replace('Bytes', 'bps')
            .replace('B', 'bps'))


def start_server(core):
    global app_thread

    bind_address = core.config.get(
        'web.listener.address', '127.0.0.1')
    bind_port = core.config.get(
        'web.listener.port', 8015)
    app.core = core
    if app.core.state.get('web_secret_key') is None:
        app.core.state.set(
            'web_secret_key', bleemeo_agent.util.generate_password())
    app.secret_key = app.core.state.get('web_secret_key')
    app_thread = threading.Thread(
        target=app.run,
        kwargs={'host': bind_address, 'port': bind_port}
    )
    app_thread.daemon = True
    app_thread.start()
