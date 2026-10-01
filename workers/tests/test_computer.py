import asyncio
import unittest

from yui_worker.protocol import ProtocolError
from yui_worker.workers.computer import ComputerWorker, allowed_url


class ComputerTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.worker = ComputerWorker()
        async def idle(*args):
            await asyncio.Event().wait()
        self.worker.run = idle

    async def asyncTearDown(self):
        for job in self.worker.jobs.values():
            await self.worker.cancel({'id':job['id'], 'identity_id':job['owner']})
        self.worker.loop.close()

    def payload(self):
        return {'identity_id':'owner', 'task':'Read heading', 'allowed_hosts':['example.com'], 'start_url':'https://example.com'}

    async def test_owner_isolation_cancel_and_reply(self):
        result = await self.worker.begin(self.payload())
        query = dict(id=result['id'], identity_id='other')
        with self.assertRaises(ProtocolError):
            await self.worker.status(query)
        query['identity_id']='owner'
        with self.assertRaises(ProtocolError):
            await self.worker.reply(dict(query, text='yes'))
        self.worker.jobs[result['id']]['status']='waiting_for_user'
        reply = await self.worker.reply(dict(query, text='Stop before submitting'))
        self.assertEqual(reply['status'],'running')
        result = await self.worker.cancel(query)
        self.assertEqual(result['status'],'cancelled')

    async def test_domain_scope_and_concurrent_tasks(self):
        p = self.payload(); p['start_url']='https://unapproved.example'
        with self.assertRaises(ProtocolError):
            await self.worker.begin(p)
        await self.worker.begin(self.payload())
        with self.assertRaises(ProtocolError):
            await self.worker.begin(self.payload())

    def test_exact_hosts_and_no_local_schemes(self):
        hosts={'example.com','localhost','127.0.0.1','192.168.1.2'}
        self.assertTrue(allowed_url('https://example.com/page',hosts))
        for url in ['file:///etc/passwd','http://localhost','http://127.0.0.1','http://192.168.1.2','https://example.com.attacker.net','https://me:secret@example.com']:
            self.assertFalse(allowed_url(url,hosts),url)


if __name__=='__main__':
    unittest.main()
