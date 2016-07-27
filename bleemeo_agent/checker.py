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

import datetime
import imaplib
import logging
import select
import shlex
import smtplib
import socket
import struct
import time

import requests
from six.moves.urllib import parse as urllib_parse

import bleemeo_agent.util


# Must match nagios return code
STATUS_OK = 0
STATUS_WARNING = 1
STATUS_CRITICAL = 2
STATUS_UNKNOWN = 3
# Special value, means that check could not be run, e.g. due to missing port
# information
STATUS_CHECK_NOT_RUN = -1

STATUS_NAME = {
    STATUS_OK: 'ok',
    STATUS_WARNING: 'warning',
    STATUS_CRITICAL: 'critical',
    STATUS_UNKNOWN: 'unknown',
}


CHECKS_INFO = {
    'mysql': {
        'type': 'tcp',
    },
    'apache': {
        'type': 'http',
    },
    'dovecot': {
        'type': 'imap',
    },
    'elasticsearch': {
        'type': 'http',
    },
    'influxdb': {
        'type': 'http',
        'url': '/ping'
    },
    'ntp': {
        'type': 'ntp',
    },
    'openvpn': {
        'disable_persistent_socket': True,
    },
    'openldap': {
        'type': 'tcp',
    },
    'postgresql': {
        'type': 'tcp',
    },
    'rabbitmq': {
        'type': 'tcp',
        'send': 'PINGAMQP',
        'expect': 'AMQP',
    },
    'redis': {
        'type': 'tcp',
        'send': 'PING\n',
        'expect': '+PONG',
    },
    'memcached': {
        'type': 'tcp',
        'send': 'version\r\n',
        'expect': 'VERSION',
    },
    'mongodb': {
        'type': 'tcp',
    },
    'nginx': {
        'type': 'http',
    },
    'postfix': {
        'type': 'smtp',
    },
    'exim': {
        'type': 'smtp',
    },
    'squid': {
        'type': 'http',
        '4xx_is_ok': True,
    },
    'varnish': {
        'type': 'tcp',
        'send': 'ping\n',
        'expect': 'PONG'
    },
    'zookeeper': {
        'type': 'tcp',
        'send': 'ruok\n',
        'expect': 'imok',
    },
}


# global variable with all checks created
CHECKS = []


def update_checks(core):
    global CHECKS
    for check in CHECKS:
        check.stop()

    CHECKS = []

    for key, service_info in core.services.items():
        (service_name, instance) = key
        try:
            new_check = Check(
                core,
                service_name,
                instance,
                service_info,
            )
            CHECKS.append(new_check)
        except NotImplementedError:
            logging.debug(
                'No check exists for service %s', service_name,
            )
        except:
            logging.debug(
                'Failed to initialize check for service %s',
                service_name,
                exc_info=True
            )


def periodic_check():
    """ Run few periodic check:

        * that all TCP socket are still openned
    """
    for check in CHECKS:
        check.check_sockets()


class Check:
    def __init__(self, core, service_name, instance, service_info):
        self.address = service_info.get('address')
        self.port = service_info.get('port')
        self.protocol = service_info.get('protocol')

        self.check_info = CHECKS_INFO.get(service_name, {})

        if self.port is not None and self.protocol == socket.IPPROTO_TCP:
            self.check_info.setdefault('type', 'tcp')

        if (service_info.get('password') is None
                and service_name in ('mysql', 'postgresql')):
            # For those check, if password is not set the dedicated check
            # will fail.
            self.check_info['type'] = 'tcp'

        self.service = service_name
        self.instance = instance
        self.service_info = service_info
        self.core = core

        self.extra_ports = self.service_info.get('extra_ports', {})

        if self.service_info.get('check_type') is not None:
            self.check_info = {'type': self.service_info['check_type']}
        if self.service_info.get('check_command') is not None:
            self.check_info['check_command'] = (
                self.service_info['check_command']
            )

        if not self.check_info and not self.extra_ports:
            raise NotImplementedError("No check for this service")

        logging.debug(
            'Created new check for service %s (on %s)',
            self.service,
            self.instance,
        )

        self.tcp_sockets = self._initialize_tcp_sockets()

        self.last_run = time.time()

        self.current_job = self.core.scheduler.add_interval_job(
            self.run_check,
            start_date=datetime.datetime.now() + datetime.timedelta(seconds=1),
            seconds=60,
        )
        self.open_sockets_job = None

    def _initialize_tcp_sockets(self):
        tcp_sockets = {}

        if self.port is not None and self.protocol == socket.IPPROTO_TCP:
            tcp_sockets[(self.address, self.port)] = None

        for port_protocol, address in self.extra_ports.items():
            if not port_protocol.endswith('/tcp'):
                continue

            port = int(port_protocol.split('/')[0])
            if port == self.port:
                continue
            tcp_sockets[(address, port)] = None

        return tcp_sockets

    def open_sockets(self):
        """ Try to open all closed sockets
        """
        if self.check_info.get('disable_persistent_socket'):
            return

        run_check = False

        for (key, tcp_socket) in self.tcp_sockets.items():
            (address, port) = key

            if tcp_socket is not None:
                continue

            tcp_socket = socket.socket()
            tcp_socket.settimeout(2)
            try:
                tcp_socket.connect((address, port))
                self.tcp_sockets[(address, port)] = tcp_socket
            except socket.error:
                tcp_socket.close()
                logging.debug(
                    'check %s (on %s): failed to open socket to %s:%s',
                    self.service, self.instance, address, port
                )
                run_check = True

        if run_check:
            # open_socket failed, run check now
            # reschedule job to be run immediately
            self.core.scheduler.unschedule_job(self.current_job)
            self.current_job = self.core.scheduler.add_interval_job(
                self.run_check,
                start_date=(
                    datetime.datetime.now() +
                    datetime.timedelta(seconds=1)
                ),
                seconds=60,
            )

    def check_sockets(self):
        """ Check if some socket are closed
        """
        try_reopen = False

        sockets = {}
        for key, sock in self.tcp_sockets.items():
            if sock is not None:
                sockets[sock] = key

        (rlist, _, _) = select.select(sockets.keys(), [], [], 0)
        for s in rlist:
            try:
                buffer = s.recv(65536)
            except socket.error:
                buffer = b''

            if buffer == b'':
                (address, port) = sockets[s]
                logging.debug(
                    'check %s (on %s) : connection to %s:%s closed',
                    self.service, self.instance, address, port
                )
                s.close()
                self.tcp_sockets[(address, port)] = None
                try_reopen = True

        if try_reopen:
            self.open_sockets()

    def run_check(self):  # noqa
        self.last_run = time.time()

        if self.address is None and self.instance is not None:
            # Address is None if this check is associated with a stopped
            # container. In such case none of our test could pass
            (return_code, output) = (
                STATUS_CRITICAL, 'Container stopped: connection refused'
            )
        elif self.check_info.get('type') == 'nagios':
            (return_code, output) = self.check_nagios()
        elif self.check_info.get('type') == 'tcp':
            (return_code, output) = self.check_tcp()
        elif self.check_info.get('type') == 'http':
            (return_code, output) = self.check_http()
        elif self.check_info.get('type') == 'imap':
            (return_code, output) = self.check_imap()
        elif self.check_info.get('type') == 'smtp':
            (return_code, output) = self.check_smtp()
        elif self.check_info.get('type') == 'ntp':
            (return_code, output) = self.check_ntp()
        else:
            (return_code, output) = (STATUS_CHECK_NOT_RUN, '')

        if (return_code != STATUS_CRITICAL
                and return_code != STATUS_UNKNOWN
                and self.extra_ports):
            for (address, port) in self.tcp_sockets:
                if port == self.port:
                    # self.port is already checked with above check
                    continue
                (extra_port_rc, extra_port_output) = self.check_tcp(
                    address, port)
                if extra_port_rc == STATUS_CRITICAL:
                    (return_code, output) = (extra_port_rc, extra_port_output)
                    break
                if return_code == STATUS_CHECK_NOT_RUN:
                    return_code = extra_port_rc
                    output = extra_port_output

        if return_code == STATUS_CHECK_NOT_RUN:
            logging.debug(
                'check %s (on %s): no check available. Not metric sent',
                self.service, self.instance,
            )
            return

        logging.debug(
            'check %s (on %s): return code is %s (output=%s)',
            self.service, self.instance, return_code, output,
        )

        metric = {
            'measurement': '%s_status' % self.service,
            'status': STATUS_NAME[return_code],
            'service': self.service,
            'time': self.last_run,
            'value': float(return_code),
            'check_output': output,
        }
        if self.instance is not None:
            metric['item'] = self.instance
            metric['instance'] = self.instance
        self.core.emit_metric(metric)

        if return_code != STATUS_OK:
            # close all TCP sockets
            for key, sock in self.tcp_sockets.items():
                if sock is not None:
                    sock.close()
                    self.tcp_sockets[key] = None

        if return_code == STATUS_OK and self.tcp_sockets:
            # Make sure all socket are openned
            self.open_sockets_job = self.core.scheduler.add_date_job(
                self.open_sockets,
                date=(
                    datetime.datetime.now() + datetime.timedelta(seconds=5)
                ),
            )

    def stop(self):
        """ Unschedule this check
        """
        logging.debug('Stoping check %s (on %s)', self.service, self.instance)
        try:
            self.core.scheduler.unschedule_job(self.open_sockets_job)
        except KeyError:
            logging.debug(
                'Job open_socket for check %s (on %s) was already unscheduled',
                self.service, self.instance
            )
        self.core.scheduler.unschedule_job(self.current_job)
        for tcp_socket in self.tcp_sockets.values():
            if tcp_socket is not None:
                tcp_socket.close()

    def check_nagios(self):
        (return_code, output) = bleemeo_agent.util.run_command_timeout(
            shlex.split(self.check_info['check_command']),
        )

        output = output.decode('utf-8', 'ignore').strip()
        if return_code > STATUS_UNKNOWN or return_code < 0:
            return_code = STATUS_UNKNOWN

        return (return_code, output)

    def check_tcp_recv(self, sock, start):
        received = ''
        while not self.check_info['expect'] in received:
            try:
                tmp = sock.recv(4096)
            except socket.timeout:
                return (
                    STATUS_CRITICAL,
                    'Connection timed out after 10 seconds'
                )
            if tmp == b'':
                break
            received += tmp.decode('utf8', 'ignore')

        if self.check_info['expect'] not in received:
            if received == '':
                return (STATUS_CRITICAL, 'No data received from host')
            else:
                return (
                    STATUS_CRITICAL,
                    'Unexpected response: %s' % received
                )

        sock.close()
        end = time.time()
        return (STATUS_OK, 'TCP OK - %.3f second response time' % (end-start))

    def check_tcp(self, address=None, port=None):  # noqa
        if address is not None or port is not None:
            use_default = False
        else:
            address = self.address
            port = self.port
            use_default = True

        if port is None or address is None:
            return (STATUS_CHECK_NOT_RUN, '')

        start = time.time()
        sock = socket.socket()
        sock.settimeout(10)
        try:
            sock.connect((address, port))
        except socket.timeout:
            return (
                STATUS_CRITICAL,
                'TCP port %d, connection timed out after 10 seconds' % port
            )
        except socket.error:
            return (STATUS_CRITICAL, 'TCP port %d, Connection refused' % port)

        if (self.check_info is not None
                and self.check_info.get('send')
                and use_default):
            try:
                sock.send(self.check_info['send'].encode('utf8'))
            except socket.timeout:
                return (
                    STATUS_CRITICAL,
                    'TCP port %d, connection timed out after 10 seconds' % port
                )
            except socket.error:
                return (
                    STATUS_CRITICAL,
                    'TCP port %d, connection closed too early' % port
                )

        if (self.check_info is not None
                and self.check_info.get('expect')
                and use_default):
            return self.check_tcp_recv(sock, start)

        sock.close()
        end = time.time()
        return (STATUS_OK, 'TCP OK - %.3f second response time' % (end-start))

    def check_http(self):
        if self.port is None or self.address is None:
            return (STATUS_CHECK_NOT_RUN, '')

        base_url = 'http://%s:%s' % (self.address, self.port)
        url = urllib_parse.urljoin(base_url, self.check_info.get('url', '/'))
        start = time.time()
        try:
            response = requests.get(url, timeout=10, allow_redirects=False)
        except requests.exceptions.Timeout:
            return (STATUS_CRITICAL, 'Connection timed out after 10 seconds')
        except requests.exceptions.RequestException:
            return (STATUS_CRITICAL, 'Connection refused')

        end = time.time()

        if response.status_code >= 500:
            return (
                STATUS_CRITICAL,
                'HTTP CRITICAL - http_code=%s / %.3f second response time' % (
                    response.status_code,
                    end-start,
                )
            )
        elif (response.status_code >= 400
                and not self.check_info.get('4xx_is_ok', False)):
            return (
                STATUS_WARNING,
                'HTTP WARN - status_code=%s / %.3f second response time' % (
                    response.status_code,
                    end-start,
                )
            )
        else:
            return (
                STATUS_OK,
                'HTTP OK - %.3f second response time' % (end-start)
            )

    def check_imap(self):
        if self.port is None or self.address is None:
            return (STATUS_CHECK_NOT_RUN, '')

        start = time.time()

        try:
            client = IMAP4Timeout(self.address, self.port)
            client.noop()
            client.logout()
        except (imaplib.IMAP4.error, socket.error):
            return (
                STATUS_CRITICAL,
                'Unable to connect to IMAP server',
            )
        except socket.timeout:
            return (
                STATUS_CRITICAL,
                'Connection timed out after 10 seconds',
            )

        end = time.time()
        return (STATUS_OK, 'IMAP OK - %.3f second response time' % (end-start))

    def check_smtp(self):
        if self.port is None or self.address is None:
            return (STATUS_CHECK_NOT_RUN, '')

        start = time.time()

        try:
            client = smtplib.SMTP(self.address, self.port, timeout=10)
            client.noop()
            client.quit()
        except (smtplib.SMTPException, socket.error):
            return (
                STATUS_CRITICAL,
                'Unable to connect to SMTP server',
            )
        except socket.timeout:
            return (
                STATUS_CRITICAL,
                'Connection timed out after 10 seconds',
            )

        end = time.time()
        return (STATUS_OK, 'SMTP OK - %.3f second response time' % (end-start))

    def check_ntp(self):
        if self.port is None or self.address is None:
            return (STATUS_CHECK_NOT_RUN, '')

        # Ntp use 1900-01-01 00:00:00 as epoc.
        # Since Unix use 1970-01-01 as epoc, we have this delta
        NTP_DELTA = 2208988800

        start = time.time()

        client = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        client.settimeout(10)

        msg = b'\x1b' + 47 * b'\0'
        try:
            client.sendto(msg, (self.address, self.port))
            msg, address = client.recvfrom(1024)
        except socket.timeout:
            return (STATUS_CRITICAL, 'Connection timed out after 10 seconds')

        unpacked = struct.unpack("!BBBB11I", msg)
        stratum = unpacked[1]
        server_time = unpacked[11] - NTP_DELTA

        end = time.time()

        if stratum == 0 or stratum == 16:
            return (STATUS_CRITICAL, 'NTP server not (yet) synchronized')
        elif abs(server_time - end) > 10:
            return (STATUS_CRITICAL, 'Local time and NTP time does not match')
        else:
            return (
                STATUS_OK, 'NTP OK - %.3f second response time' % (end-start)
            )


class IMAP4Timeout(imaplib.IMAP4):
    """ IMAP4 with timeout of 10 second
    """

    def open(self, host='', port=imaplib.IMAP4_PORT):
        self.host = host
        self.port = port
        self.sock = socket.create_connection((host, port), timeout=10)
        self.file = self.sock.makefile('rb')
