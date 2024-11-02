# Inside-Out

---

## About

Inside-Out is a proxy and protocol transformer that can be used to open access to backend HTTP services without directly exposing the ports of those services.

The client request will hit the Out service (exposed to the outside in a DMZ), where the request will be broken apart and placed in a RabbitMQ queue. The In service (inside the core network) watches the queue, executes the request against the configured "real" backend, gathers the response and publishes the response on another queue where the Out component can pick it up and reply to the original request.

## Examples

- Consider a backend service inside of a secured network. The network is configured with a DMZ where some services are exposed publicly. A typical solution is to open a port into the DMZ to a proxy and then open another port from the proxy to the backend service.
  
  With Inside-Out, the Out service and the queue reside in the DMZ. There are no connections initiated from the DMZ into the secure network, no firewall ports need to be opened, no network change requests, no red-tape, etc. Thus you can allow exposing secured HTTP services to the internet without allowing internet connections directly to the service.

- Another example where I think it could be useful is for like a SaaS API gateway. A customer could run the In service inside a secured network, never exposing the backend services directly, yet still serve API's through the SaaS.

## Design

Each HTTP request in Go is handled by a goroutine, so leveraging that each response to the request fires off a blocking channel listen for the response in the queue. A correlation ID is created for every request to make sure the right response is return to the corresponding request.

The message bodies are base64 encoded so that any HTTP request/response should be fine without jumping through content encoding hoops. RabbitMQ messages are binary to begin with but the choice of base64 encoding was just to account for other potential queuing solutions that transmit text.

## Usage

Configuration is through environment variables, whether is is being run in a container or at the command line. See the ``setenv.sh`` file for the variables that need to be set for both services.

There is a dependency on RabbitMQ. The services will create the queues they need so they need to be configured with accounts that allow them to do so.

Once the queue is running, the configuration variables are set, then it should just work. The Out service will listen on the configured TCP port and any queries sent to it on that port should act the same as if you were sending requests to the backend directly.

## Performance

I'm sure there are many changes that could increase performance, but the implementation has focused on simplicity.

Go channels are use extensively (no mutexes) so it should scale to the full capabilities of the machine (or limits given in k8s).

RabbitMQ is probably more powerful that what is needed so a more simple queuing solution (perhaps networked Go channels like [netchan](https://github.com/matveynator/netchan)) could increase performance and reduce complexity.

A modest laptop running a fast backend should handle thousands of requests per second.

## Scalability

Both the In and Out services can be run with many instances in parallel given the same configuration.

The In service just requires more instances of it running while multiple Out services would need to be placed behind a load balancer.

## Limits

It is not possible to split out backends based on request parameters. All requests will be spread among the configured backends (a single backend is, of course, also fine). If new backends need to be exposed then new In and Out services will need to be configured and spun up. Note that the request and response exchanges and queue names will also need to be different if connected to the same queue server.

## TODO

- There are some notes in the code
- Although I have used this code successfully in Prod, there are some fragile bits that need attention
- Do some profiling
