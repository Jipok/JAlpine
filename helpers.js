// When Alpine components have been initialized, merge our data
document.addEventListener('alpine:initialized', () => {
    document.querySelectorAll('[x-data]').forEach(el => {
        const componentName = el.getAttribute('x-data');
        const compData = window._componentData[componentName];
        if (!compData) return;
        Object.assign(Alpine.$data(el), compData);
        delete window._componentData[componentName];
    });

    // Check for any leftover component data and output error if present
    if (Object.keys(window._componentData).length > 0) {
        console.error("Unused _componentData found:", window._componentData);
    }
});


// Define AJAX helper functions for Alpine
document.addEventListener('alpine:init', () => {
    Alpine.magic('get', (el) => async(url) => {
        return makeRequest(el, 'GET', url);
    });
    Alpine.magic('post', (el) => async(url, data) => {
        return makeRequest(el, 'POST', url, data);
    });
    Alpine.magic('delete', (el) => async(url, data) => {
        return makeRequest(el, 'DELETE', url, data);
    });
    Alpine.magic('put', (el) => async(url, data) => {
        return makeRequest(el, 'PUT', url, data);
    });
    Alpine.magic('patch', (el) => async(url, data) => {
        return makeRequest(el, 'PATCH', url, data);
    });

    // Helper function for making AJAX requests
    async function makeRequest(el, method, url, data = null) {
        try {
            const options = {
                method,
                headers: {
                    'Content-Type': 'application/json',
                }
            };

            if (data) {
                options.body = JSON.stringify(data);
            }

            const response = await fetch(url, options);
            const responseData = await response.json();

            if (response.ok) {
                // Update the current Alpine component
                const currentScope = Alpine.$data(el);
                Object.entries(responseData).forEach(([key, value]) => {
                    if (!key.includes('::')) {
                        currentScope[key] = value;
                    }
                });

                // For namespaced data, update corresponding components
                document.querySelectorAll('[x-data]').forEach(element => {
                    const compName = element.getAttribute('x-data');
                    const scope = Alpine.$data(element);
                    Object.entries(responseData).forEach(([key, value]) => {
                        if (key.includes('::')) {
                            const [targetComp, field] = key.split('::');
                            if (targetComp === compName) {
                                scope[field] = value;
                            }
                        }
                    });
                });
                return responseData;
            } else {
                throw new Error(responseData.error || 'Request failed');
            }
        } catch (error) {
            console.error('API request failed:', error);
            const scope = Alpine.$data(el);
            scope.error = error.message;
            throw error;
        }
    }
});