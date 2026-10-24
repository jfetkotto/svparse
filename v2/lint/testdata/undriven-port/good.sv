module top (input clk, input d, output logic q, output wire w, output reg [1:0] r, output _spare, inout io);
  always_ff @(posedge clk) q <= d;
  assign w = d;
  assign r[0] = d;
  assign r[1] = d;
  assign io = d;
endmodule
module wrap (output o); inner u(.o(o)); endmodule
module inner (output o); assign o = 1; endmodule
