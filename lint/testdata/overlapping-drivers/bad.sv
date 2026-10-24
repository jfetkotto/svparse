module top (input clk, input [7:0] a, input [3:0] b, output logic [7:0] q);
  logic [7:0] v;
  logic [3:0] mem [4];
  assign v[3:0] = a[3:0];
  assign v[5:2] = a[5:2];                // want
  always_ff @(posedge clk) q[7:4] <= b;
  always_ff @(posedge clk) q[5:0] <= a[5:0];  // want
  always_comb mem[1] = b;
  assign mem[1] = b;                     // want
  logic [15:0] w;
  assign w[0 +: 8] = a;
  assign w[7 -: 2] = b[1:0];             // want
  for (genvar i = 0; i < 2; i++) begin : g
    logic [1:0] t;
    assign t[0] = a[i];
    assign t[0] = b[i];                  // want
  end
endmodule
